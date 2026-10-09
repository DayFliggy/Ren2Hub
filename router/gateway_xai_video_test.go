package router

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xAI bills a video its moderation blocks and a failed request that reports a
// cost, so those tasks settle as charged on an xAI channel; any other failure
// is refunded. A result settles on its completion facts merged over the
// frozen submit facts, so a fact the result omits keeps its requested value.
func TestGatewayXAIVideoSettlesChargedModerationAndRefundsFailures(t *testing.T) {
	const videoModel = "grok-imagine-video-1.5"
	const expression = `u("moderated") == true ? tier("blocked", 0.05 + u("seconds") * 0) : ` +
		`u("resolution") == "1080p" ? tier("1080p", u("seconds") * 0.25 + u("input_images") * 0.01) : ` +
		`u("resolution") == "720p" ? tier("720p", u("seconds") * 0.14 + u("input_images") * 0.01) : ` +
		`tier("480p", u("seconds") * 0.08 + u("input_images") * 0.01)`
	var sends atomic.Int32
	base, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		send := sends.Add(1)
		assert.Equal(t, "/v1/videos/generations", r.URL.Path)
		assert.Equal(t, "Bearer xai-fixture-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"request_id":"xai-job-%d"}`, send)
	})
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models, channel.Key = constant.ChannelTypeXai, videoModel, "xai-fixture-key"
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "key": channel.Key}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	require.NoError(t, setGatewayModelPrice(videoModel, model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}))
	plugin, ok := jsplugin.DefaultRegistry.Get("xai")
	require.True(t, ok)
	// 8 seconds at 720p plus one input image reserve 1.13 USD.
	const estimate = common.Quota(565000)

	for _, test := range []struct {
		name, completion string
		wantRecovery     string
		wantCharge       common.Quota
		wantModerated    bool
	}{
		{"moderated output is charged at the blocked tier",
			`{"status":"done","progress":100,"video":{"url":"","duration":8,"respect_moderation":false},"usage":{"cost_in_usd_ticks":11200000000}}`,
			model.BillingRecoveryStatusSettled, 25000, true},
		// The failure reports no length; the blocked tier reads the requested
		// seconds from the submit facts and charges the blocked fee only.
		{"billed failure is charged at the blocked tier",
			`{"status":"failed","error":{"code":"invalid_argument","message":"Content blocked by moderation"},"usage":{"cost_in_usd_ticks":500000000}}`,
			model.BillingRecoveryStatusSettled, 25000, true},
		// xAI reports no resolution; the requested 720p prices the delivered
		// 6 seconds plus the input image (0.85 USD).
		{"delivered video keeps the requested resolution",
			`{"status":"done","progress":100,"video":{"url":"https://vidgen.x.ai/v.mp4","duration":6,"respect_moderation":true},"usage":{"cost_in_usd_ticks":8500000000}}`,
			model.BillingRecoveryStatusSettled, 425000, false},
		{"unbilled failure is refunded",
			`{"status":"failed","error":{"code":"internal_error","message":"engine crashed"},"usage":{"cost_in_usd_ticks":0}}`,
			model.BillingRecoveryStatusRefunded, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := sends.Load()
			code, body := postGatewayTask(t, base, key, "/v1/videos",
				fmt.Sprintf(`{"model":%q,"prompt":"a fox","input_reference":"https://cdn.example/fox.png","seconds":"8","size":"1280x720"}`, videoModel))
			require.Equal(t, http.StatusOK, code, body)
			assert.Equal(t, before+1, sends.Load())
			var task model.Task
			require.NoError(t, model.DB.Order("id desc").First(&task).Error)
			require.Equal(t, estimate, task.Quota)
			var user model.User
			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			usedBefore := user.UsedQuota

			result, err := taskplugin.New(plugin).ParseTaskResultForTask(&task, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, []byte(test.completion))
			require.NoError(t, err)
			require.Empty(t, result.UsageError)
			assert.Equal(t, test.wantModerated, result.UsageFacts["moderated"] == true)
			for range 2 {
				require.NoError(t, service.CompleteImmediateTask(context.Background(), &task, result))
			}

			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
			assert.Equal(t, test.wantRecovery, recovery.Status)
			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			assert.Zero(t, user.ReservedQuota)
			assert.Equal(t, test.wantCharge, user.UsedQuota-usedBefore)
		})
	}
}
