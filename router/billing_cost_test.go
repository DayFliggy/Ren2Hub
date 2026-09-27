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

func TestBillingCostAPIRequiresRootAndPreservesCustomerFunds(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.BillingUpstreamCost{}))
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	for index, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		name := fmt.Sprintf("cost-api-%d", index+1)
		pat := name + "-pat"
		require.NoError(t, model.DB.Create(&model.User{Id: index + 1, Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled, AccessToken: &pat, Quota: 100}).Error)
	}
	ctx := context.Background()
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: "cost-api-request", UserID: 1, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, model.Reserve("cost-api-request", 20))
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "cost-model", Mode: billing.ModeFree}, "1", nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, model.PrepareBillingAttempt(ctx, "cost-api-request", "cost-api-attempt", 11, quote))
	require.NoError(t, model.UpdateBillingRecoverySettlement("cost-api-request", 0))
	require.NoError(t, model.Settle("cost-api-request", 20))
	engine := gin.New()
	SetApiRouter(engine)
	call := func(actor int, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/billing/reconciliations/cost-api-request/cost", strings.NewReader(body))
		if actor > 0 {
			request.Header.Set("Authorization", fmt.Sprintf("Bearer cost-api-%d-pat", actor))
		}
		request.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, request)
		return recorder
	}
	body := `{"event_id":"receipt-cost-api","attempt_id":"cost-api-attempt","amount_usd":"0.00001","source":"invoice","reference":"Invoice 803 line 4"}`
	assert.Equal(t, http.StatusUnauthorized, call(0, body).Code)
	for _, actor := range []int{1, 2} {
		assert.Equal(t, http.StatusForbidden, call(actor, body).Code)
	}
	for range 2 {
		response := call(3, body)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), `"amount_usd":"0.00001"`)
	}
	assert.Equal(t, http.StatusConflict, call(3, strings.Replace(body, "0.00001", "0.00002", 1)).Code)
	assert.Equal(t, http.StatusBadRequest, call(3, strings.Replace(body, `"0.00001"`, "0.00001", 1)).Code)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.Zero(t, user.ReservedQuota)
	assert.EqualValues(t, 20, user.UsedQuota)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingUpstreamCost{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
