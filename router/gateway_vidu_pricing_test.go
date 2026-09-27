package router

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayViduFixedPriceReservesAndSettlesOnce(t *testing.T) {
	var sends atomic.Int32
	base, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		assert.Equal(t, "/ent/v2/text2video", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task_id":"vidu-provider-job","state":"created"}`)
	})
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models = constant.ChannelTypeVidu, "viduq2"
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	price, err := service.ValidateTaskPluginPricePublication(billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		Model: "viduq2", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.25"}},
	}, "vidu")
	require.NoError(t, err)
	published, err := model.PublishBillingPrice(context.Background(), price, 0, 1)
	require.NoError(t, err)
	code, body := postGatewayTask(t, base, key, "/v1/videos", `{"model":"viduq2","prompt":"isolated fixture","duration":5,"resolution":"720p"}`)
	require.Equal(t, http.StatusOK, code, body)
	assert.EqualValues(t, 1, sends.Load())
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	require.NotNil(t, task.PrivateData.BillingContext)
	assert.Equal(t, published.ID, task.PrivateData.BillingContext.Quote.PriceVersion.ID)
	assert.Nil(t, task.PrivateData.BillingContext.Quote.PriceVersion.UsageSchema)
	assert.EqualValues(t, 125000, task.Quota)
	var user model.User
	require.NoError(t, model.DB.First(&user, task.UserId).Error)
	assert.EqualValues(t, 125000, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
	plugin, ok := jsplugin.DefaultRegistry.Get("vidu")
	require.True(t, ok)
	adaptor := taskplugin.New(plugin)
	result, err := adaptor.ParseTaskResultForTask(&task, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, []byte(`{"state":"success","credits":10}`))
	require.NoError(t, err)
	require.Empty(t, result.UsageError)
	for range 2 {
		require.NoError(t, service.CompleteImmediateTask(context.Background(), &task, result))
	}
	require.NoError(t, model.DB.First(&user, task.UserId).Error)
	assert.Zero(t, user.ReservedQuota)
	assert.EqualValues(t, 125000, user.UsedQuota)
	assert.EqualValues(t, 10000000-125000, user.Quota)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.NotNil(t, stored.PrivateData.CompletionUsage)
	assert.Equal(t, "10", stored.PrivateData.CompletionUsage.Facts["credits"])
	assert.Empty(t, stored.PrivateData.BillingContext.Usage.ActualFacts)
	assert.Equal(t, "0.25", stored.PrivateData.BillingContext.Charge.TotalUSD)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", task.PrivateData.SubmissionRequestID, "settle").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, model.DB.Model(&model.TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	var token model.Token
	require.NoError(t, model.DB.First(&token, task.PrivateData.TokenId).Error)
	assert.Zero(t, token.ReservedQuota)
	assert.Equal(t, common.Quota(125000), token.UsedQuota)
}
