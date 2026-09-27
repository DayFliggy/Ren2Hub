package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayChannelProbeUsesDurableSendAndRecoveryEligibility(t *testing.T) {
	var sends atomic.Int32
	setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		var count int64
		if err := model.DB.Model(&model.BillingAttempt{}).Count(&count).Error; err != nil || count == 0 {
			t.Error("probe arrived without durable billing attempt", err)
		}
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	})
	var channel model.Channel
	require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeOpenAI).First(&channel).Error)
	request := func() bool {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/1?model=gpt-4o-mini", nil)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.Id)}}
		c.Set("id", 1)
		controller.TestChannel(c)
		var result struct {
			Success bool
			Message string
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		t.Log(result.Message)
		return result.Success
	}
	require.NoError(t, model.DB.Model(&channel).Update("status", common.ChannelStatusAutoDisabled).Error)
	require.True(t, request(), "automatic disable must remain probeable")
	assert.EqualValues(t, 1, sends.Load())
	require.NoError(t, model.DB.Model(&channel).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.False(t, request())
	assert.EqualValues(t, 1, sends.Load(), "manual disable must not send")
	awaitGatewayPermitsReleased(t)
}

func TestGatewayChannelProbeMissingUsageRemainsReserved(t *testing.T) {
	setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	})
	old, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
	require.NoError(t, err)
	_, err = model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "gpt-4o-mini", Mode: billing.ModeExpression, Expression: "p * 2 + c * 4"}, old.ID, 1)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/test/1?model=gpt-4o-mini", nil)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Set("id", 1)
	controller.TestChannel(c)
	var result struct {
		Success bool
		Message string
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	assert.False(t, result.Success, result.Message)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Zero(t, user.UsedQuota)
	assert.Greater(t, user.ReservedQuota, common.Quota(0))
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusReconciliation, recovery.Status)
	awaitGatewayPermitsReleased(t)
}
