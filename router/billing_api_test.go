package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingAPIOwnerIsolationAndRetiredReconciliation(t *testing.T) {
	setupRelayRouterTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	access := map[int]string{}
	for index, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		name := fmt.Sprintf("billing-role-%d", index+1)
		require.NoError(t, model.DB.Create(&model.User{Id: index + 1, Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled, Quota: 100}).Error)
		access[index+1] = createRouterBrowserSession(t, index+1)
		_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: name, UserID: index + 1, Source: "wallet"})
		require.NoError(t, err)
		require.NoError(t, model.Reserve(name, 20))
	}
	engine := gin.New()
	SetApiRouter(engine)
	call := func(actor int, method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if actor > 0 {
			request.Header.Set("Authorization", "Bearer "+access[actor])
		}
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)
		return recorder
	}

	// Bill-first has no operator review queue; even root cannot reach it.
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/billing/reconciliations"},
		{http.MethodGet, "/api/billing/reconciliations/billing-role-1"},
		{http.MethodPost, "/api/billing/reconciliations/billing-role-1/retry"},
		{http.MethodPost, "/api/billing/reconciliations/billing-role-1/resolve"},
		{http.MethodPost, "/api/billing/reconciliations/billing-role-1/cost"},
		{http.MethodGet, "/api/billing/metrics"},
		{http.MethodGet, "/api/billing/prices"},
		{http.MethodGet, "/api/billing/prices/automatic"},
		{http.MethodGet, "/api/billing/prices/seed"},
	} {
		assert.Equal(t, http.StatusNotFound, call(3, route.method, route.path, `{}`).Code, route.path)
	}

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/option/model_pricing"},
		{http.MethodPatch, "/api/option/model_pricing"},
		{http.MethodPost, "/api/option/model_pricing/preview"},
		{http.MethodPost, "/api/option/model_pricing/simulate"},
		{http.MethodPost, "/api/ratio_sync/fetch"},
	} {
		assert.Equal(t, http.StatusUnauthorized, call(0, route.method, route.path, `{}`).Code, route.path)
		for _, actor := range []int{1, 2} {
			assert.Equal(t, http.StatusForbidden, call(actor, route.method, route.path, `{}`).Code, route.path)
		}
	}
	preview := call(3, http.MethodPost, "/api/option/model_pricing/preview", `{"model_name":"preview-only","pricing":{"ModelPrice":"0.1"}}`)
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var snapshot struct {
		Success bool                       `json:"success"`
		Data    model.ModelPricingSnapshot `json:"data"`
	}
	require.NoError(t, common.Unmarshal(call(3, http.MethodGet, "/api/option/model_pricing", "").Body.Bytes(), &snapshot))
	require.True(t, snapshot.Success)
	assert.Empty(t, snapshot.Data.Entries, "price previews must not configure an empty installation")
	var evidenceCount int64
	require.NoError(t, model.DB.Model(&model.BillingPriceVersion{}).Count(&evidenceCount).Error)
	assert.Zero(t, evidenceCount, "price previews must not create execution evidence")

	assert.Equal(t, http.StatusUnauthorized, call(0, http.MethodGet, "/api/billing/self/reservations", "").Code)
	assert.Equal(t, http.StatusUnauthorized, call(0, http.MethodGet, "/api/billing/self/events", "").Code)
	var reservations struct {
		Success bool `json:"success"`
		Data    struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	response := call(1, http.MethodGet, "/api/billing/self/reservations", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &reservations))
	require.True(t, reservations.Success)
	require.Len(t, reservations.Data.Items, 1)
	assert.EqualValues(t, 1, reservations.Data.Total)
	item := reservations.Data.Items[0]
	assert.Equal(t, "billing-role-1", item["request_id"])
	assert.Equal(t, model.BillingRecoveryStatusActive, item["state"])
	assert.Equal(t, "20", item["funding_amount"])
	assert.NotContains(t, item, "manual_review_required")

	var events struct {
		Success bool `json:"success"`
		Data    struct {
			Items []model.BillingEvent `json:"items"`
			Total int64                `json:"total"`
		} `json:"data"`
	}
	response = call(1, http.MethodGet, "/api/billing/self/events", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &events))
	require.True(t, events.Success)
	require.Len(t, events.Data.Items, 1)
	assert.Equal(t, "billing-role-1", events.Data.Items[0].RequestID)
	assert.Equal(t, 1, events.Data.Items[0].UserID)
	assert.EqualValues(t, 20, events.Data.Items[0].ReservedDelta)
}
