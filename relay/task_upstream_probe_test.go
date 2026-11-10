//go:build upstream_probe

package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ResolveOriginTask writes inherited ratios before pricing. This probe starts
// at that seam so the earlier native-remix metadata failure cannot mask A7.
// Desired behavior remains a failing opt-in probe until upstream repairs it.
func TestUpstreamRemixPreservesInheritedRatiosUnderAdaptorEstimate(t *testing.T) {
	service.InitHttpClient()
	const source = `
export const meta = {apiVersion:1,key:"remix-ratio",name:"Remix Ratio",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second",description:"Video generation unit price"}}};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl+"/submit", method:"POST", body:{model: ctx.model}, action:"remix"}; }
export function parseSubmitResponse(ctx, response) { return {taskId: response.body.id}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
export function extractUsage() { return {seconds:5}; }
`
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"declared-model":0.1}`))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"job"}`))
	}))
	defer server.Close()

	c, info := newTaskSubmitContext(t, "declared-model", "")
	pinMappingOrderPlugin(t, c, source)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	info.OriginModelName = "declared-model"
	info.Billing = &imageReservation{limit: 1 << 30}
	info.PriceData.AddOtherRatio("seconds", 12)
	info.PriceData.AddOtherRatio("size", 1.5)

	for attempt := 1; attempt <= 2; attempt++ {
		result, taskErr := RelayTaskSubmit(c, info)
		require.Nil(t, taskErr, "attempt %d: %+v", attempt, taskErr)
		assert.Equal(t, map[string]float64{"seconds": 5, "size": 1.5}, info.PriceData.OtherRatios())
		assert.Equal(t, 375_000, result.Quota, "attempt %d: 0.1 x 500000 x 5 x 1.5, not compounded", attempt)
	}
}

// An initialized metadata object isolates N1 from the earlier native N2 panic.
func TestUpstreamRemixResolvesVendorTaskID(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, db.Create(&model.Channel{Id: 11, Key: "test-key", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeSora}).Error)
	task := model.Task{TaskID: "task_public", UserId: 17, ChannelId: 11, PrivateData: model.TaskPrivateData{UpstreamTaskID: "video_vendor"}}
	require.NoError(t, db.Create(&task).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_public/remix", nil)
	c.Params = gin.Params{{Key: "video_id", Value: "task_public"}}
	info := &relaycommon.RelayInfo{UserId: 17, OriginModelName: "sora-2", TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 11}}
	require.Nil(t, ResolveOriginTask(c, info))
	require.Equal(t, "video_vendor", info.OriginTaskID, "known_upstream=N1; the vendor must receive its own task ID")
}

// Exercise the real built-in Sora adaptor without the blocked native resolver.
func TestUpstreamRemixAdaptorPreservesAction(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_public/remix", strings.NewReader(`{"prompt":"make it night"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "sora-2",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: constant.TaskActionRemix, OriginTaskID: "video_vendor"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "http://127.0.0.1", ChannelType: constant.ChannelTypeSora,
			ApiKey: "test-key", UpstreamModelName: "sora-2",
		},
	}
	adaptor := GetTaskAdaptor(constant.TaskPlatform("sora"))
	require.NotNil(t, adaptor)
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Equal(t, constant.TaskActionRemix, info.Action, "known_upstream=N3; native validation must preserve remix")
}
