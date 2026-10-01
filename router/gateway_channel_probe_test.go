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
	"github.com/tidwall/gjson"
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

// A probe consumes provider work and settles like a relayed request: missing
// usage settles its observed estimate, and usage that cannot be priced settles
// at the frozen pre-send estimate while the probe still reports the failure.
func TestGatewayChannelProbeSettlesWithoutConfirmedUsage(t *testing.T) {
	for _, test := range []struct {
		name, usage, expression string
		success, frozen         bool
	}{
		{"missing_usage", "", "p * 2 + c * 4", true, false},
		// Valid for the editor's smoke vectors and the output-free estimate, but
		// negative for the probe's actual five output tokens.
		{"unpriceable_usage", `,"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`, "c == 5 ? p - 100 : p * 2", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"id":"test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]`+test.usage+`}`)
			})
			_, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
			require.NoError(t, err)
			_, err = setGatewayExpression("gpt-4o-mini", test.expression)
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
			assert.Equal(t, test.success, result.Success, result.Message)
			awaitGatewayPermitsReleased(t)
			var user model.User
			var recovery model.BillingRecovery
			var attempt model.BillingAttempt
			var log model.Log
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&recovery).Error)
			require.NoError(t, model.DB.First(&attempt, "request_id = ?", recovery.RequestID).Error)
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).First(&log).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			assert.Zero(t, user.ReservedQuota)
			assert.Positive(t, user.UsedQuota)
			assert.Equal(t, user.UsedQuota, log.Quota)
			var usage billing.Usage
			require.NoError(t, common.UnmarshalJsonStr(string(recovery.UsageSnapshot), &usage))
			assert.False(t, usage.Confirmed())
			assert.Equal(t, test.frozen, usage.Source == billing.UsageSourceRequestEstimate)
			assert.Equal(t, test.frozen, gjson.Get(log.Other, "admin_info.usage_billing_path").String() == "frozen_estimate")
			if test.frozen {
				var estimate billing.ChargeResult
				require.NoError(t, common.UnmarshalJsonStr(attempt.EstimateCharge, &estimate))
				assert.Equal(t, estimate.FinalQuota, user.UsedQuota, "the frozen estimate is the charge")
				assert.Equal(t, "channel_probe_unpriced", gjson.Get(log.Other, "admin_info.billing_fallback_reason").String())
			}
		})
	}
}
