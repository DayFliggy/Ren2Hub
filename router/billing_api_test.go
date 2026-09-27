package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingAPIRootResolutionAndOwnerIsolation(t *testing.T) {
	setupRelayRouterTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	for index, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		name := fmt.Sprintf("billing-role-%d", index+1)
		pat := name + "-pat"
		require.NoError(t, model.DB.Create(&model.User{Id: index + 1, Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled, AccessToken: &pat, Quota: 100}).Error)
		_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: name, UserID: index + 1, Source: "wallet"})
		require.NoError(t, err)
		require.NoError(t, model.Reserve(name, 20))
	}
	engine := gin.New()
	SetApiRouter(engine)
	call := func(actor int, method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", fmt.Sprintf("Bearer billing-role-%d-pat", actor))
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)
		return recorder
	}
	for _, actor := range []int{1, 2} {
		assert.Equal(t, http.StatusForbidden, call(actor, http.MethodGet, "/api/billing/reconciliations", "").Code)
		assert.Equal(t, http.StatusForbidden, call(actor, http.MethodPost, "/api/billing/reconciliations/billing-role-1/resolve", `{}`).Code)
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/billing/prices/automatic"},
		{http.MethodPut, "/api/billing/prices/automatic"},
		{http.MethodPost, "/api/billing/prices/automatic/sync"},
		{http.MethodPost, "/api/billing/prices/automatic/restore"},
	} {
		for _, actor := range []int{1, 2} {
			assert.Equal(t, http.StatusForbidden, call(actor, route.method, route.path, `{}`).Code)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
	for _, path := range []string{"reservations", "events"} {
		response := call(1, http.MethodGet, "/api/billing/self/"+path, "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "billing-role-1")
		assert.NotContains(t, response.Body.String(), "billing-role-2")
		assert.NotContains(t, response.Body.String(), "billing-role-3")
	}
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", "billing-role-1").First(&recovery).Error)
	body := fmt.Sprintf(`{"operation_id":"resolve-once","expected_version":%d,"action":"settle","quota":"30","reason":"confirmed provider invoice"}`, recovery.Version)
	path := "/api/billing/reconciliations/billing-role-1/resolve"
	assert.Equal(t, http.StatusConflict, call(3, http.MethodPost, path, body).Code, "administrator amounts cannot substitute for usage evidence")
	version, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{Model: "resolution-test", Mode: billing.ModeExpression, Expression: "p * 2", Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "resolution test price"}}, 0, 1)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*version, "1", nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), recovery.RequestID, "resolution-attempt", 0, quote))
	usage := &billing.Usage{InputTokens: 30, ContextTokens: 30, Source: "provider", Complete: true, Terminal: "completed"}
	charge, err := billing.Calculate(quote, *usage)
	require.NoError(t, err)
	require.NoError(t, model.PrepareBillingRecoveryConsumption(recovery.RequestID, model.BillingConsumption{Log: model.Log{UserId: 1, ModelName: "resolution-test", Quota: 30}, Usage: usage, Charge: charge}))
	require.NoError(t, model.DB.Where("request_id = ?", recovery.RequestID).First(&recovery).Error)
	body = fmt.Sprintf(`{"operation_id":"resolve-once","expected_version":%d,"action":"settle","quota":"30","reason":"confirmed provider invoice"}`, recovery.Version)
	for range 2 {
		response := call(3, http.MethodPost, path, body)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	assert.Equal(t, http.StatusConflict, call(3, http.MethodPost, path, strings.Replace(body, `"30"`, `"31"`, 1)).Code)
	assert.Equal(t, http.StatusConflict, call(3, http.MethodPost, path, strings.Replace(body, "resolve-once", "another-operation", 1)).Code)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 70, user.Quota)
	assert.Zero(t, user.ReservedQuota)
	assert.EqualValues(t, 30, user.UsedQuota)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", "billing-role-1", "settle").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	assert.NotContains(t, call(3, http.MethodGet, "/api/billing/reconciliations", "").Body.String(), "billing-role-1")
	require.NoError(t, model.DB.Model(&recovery).Updates(map[string]any{"consumption_state": model.BillingRecoveryStatePending, "consumption_log": model.BillingRecoveryStatePending, "attempts": 1}).Error)
	assert.Contains(t, call(3, http.MethodGet, "/api/billing/reconciliations", "").Body.String(), "billing-role-1", "failed log delivery remains visible after funds settle")
}
