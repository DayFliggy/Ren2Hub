package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayFactoryCompletionFactsProtectReservedFunds(t *testing.T) {
	for _, test := range []struct {
		name, key, model, expression, submit, completion string
		channelType                                      int
		invalid                                          bool
	}{
		{"kling invalid suffix", "kling", "kling-v1", `v2:u("units") * 0.4`, `{"code":0,"data":{"task_id":"job"}}`, `{"data":{"task_status":"succeed","final_unit_deduction":"10INVALID"}}`, constant.ChannelTypeKling, true},
		{"kling confirmed zero", "kling", "kling-v1", `v2:u("units") * 0.4`, `{"code":0,"data":{"task_id":"job"}}`, `{"data":{"task_status":"succeed","final_unit_deduction":"0"}}`, constant.ChannelTypeKling, false},
		{"sora overflow", "sora", "sora-2", `v2:u("seconds") * 0.4`, `{"id":"job","status":"queued"}`, `{"status":"completed","seconds":999999}`, constant.ChannelTypeSora, true},
		{"sora confirmed zero", "sora", "sora-2", `v2:u("seconds") * 0.4`, `{"id":"job","status":"queued"}`, `{"status":"completed","seconds":0}`, constant.ChannelTypeSora, false},
		{"vidu empty string", "vidu", "viduq2", "", `{"task_id":"job","state":"created"}`, `{"state":"success","credits":""}`, constant.ChannelTypeVidu, true},
		{"vidu boolean", "vidu", "viduq2", "", `{"task_id":"job","state":"created"}`, `{"state":"success","credits":false}`, constant.ChannelTypeVidu, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.submit)
			})
			for _, id := range []int{1, 4} {
				channel, err := model.GetChannelById(id, true)
				require.NoError(t, err)
				channel.Type, channel.Models, channel.Key = test.channelType, test.model, "fixture|fixture"
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "key": channel.Key}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
			}
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			plugin, ok := jsplugin.DefaultRegistry.Get(test.key)
			require.True(t, ok)
			price := billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: test.model, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.25"}}}
			var err error
			if test.expression != "" {
				price, err = plugins.FrozenTaskPriceVersion(plugin.Meta, test.model, test.expression)
				require.NoError(t, err)
			}
			price.Provenance = &billing.PriceProvenance{Kind: "retail", RetailReason: "reviewed mock task contract"}
			price, err = service.ValidateTaskPluginPricePublication(price, test.key)
			require.NoError(t, err)
			var expected int64
			if active, lookupErr := model.GetActiveBillingPrice(context.Background(), test.model); lookupErr == nil {
				expected = active.ID
			}
			_, err = model.PublishBillingPrice(context.Background(), price, expected, 1)
			require.NoError(t, err)
			code, body := postGatewayTask(t, base, key, "/v1/videos", fmt.Sprintf(`{"model":%q,"prompt":"isolated fixture","seconds":5}`, test.model))
			require.Equal(t, http.StatusOK, code, body)
			assert.EqualValues(t, 1, sends.Load())
			var task model.Task
			require.NoError(t, model.DB.First(&task).Error)
			require.Positive(t, task.Quota)
			reserved := task.Quota
			result, err := taskplugin.New(plugin).ParseTaskResultForTask(&task, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, []byte(test.completion))
			require.NoError(t, err)
			if test.invalid {
				require.NotEmpty(t, result.UsageError)
			} else {
				require.Empty(t, result.UsageError)
			}
			for range 2 {
				err := service.CompleteImmediateTask(context.Background(), &task, result)
				if test.invalid {
					require.ErrorContains(t, err, "reconciliation required")
				} else {
					require.NoError(t, err)
				}
			}
			var user model.User
			var token model.Token
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			require.NoError(t, model.DB.First(&token, task.PrivateData.TokenId).Error)
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
			wantReserved, wantSettlements := common.Quota(0), int64(1)
			if test.invalid {
				wantReserved, wantSettlements = reserved, 0
				assert.Equal(t, model.BillingRecoveryStatusReconciliation, recovery.Status)
			} else {
				assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			}
			assert.Equal(t, wantReserved, user.ReservedQuota)
			assert.Equal(t, wantReserved, token.ReservedQuota)
			assert.Equal(t, common.Quota(10000000)-wantReserved, user.Quota)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, token.UsedQuota)
			var count int64
			require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", task.PrivateData.SubmissionRequestID, "settle").Count(&count).Error)
			assert.Equal(t, wantSettlements, count)
			require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", task.PrivateData.SubmissionRequestID, "release").Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, model.DB.Model(&model.TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}
