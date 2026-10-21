package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ratioGuardPlugin = `
export const meta = {apiVersion:1,key:"ratio-guard",name:"Ratio Guard",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second",description:"Video generation unit price"}}};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl+"/submit", method:"POST", body:{model: ctx.model}, action:"text_to_video"}; }
export function parseSubmitResponse(ctx, response) { return {taskId: response.body.id}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
export function extractUsage() { return {seconds:2}; }
`

// 渠道倍率缺失或无效时，三个定价入口都必须拒绝，不能按 0 计价放行。
func TestInvalidChannelRatioRejectedByEveryPricingEntry(t *testing.T) {
	service.InitHttpClient()
	savedRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios)) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ratio-guard-model":1}`))

	for _, ratio := range []string{"", "0", "-1", "abc", "1000.5"} {
		t.Run("ratio "+ratio, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("channel_ratio", ratio)

			textInfo := &relaycommon.RelayInfo{OriginModelName: "ratio-guard-model"}
			_, err := helper.ModelPriceHelper(c, textInfo, 100, &types.TokenCountMeta{})
			require.ErrorContains(t, err, "selected channel ratio is invalid")
			assert.Zero(t, textInfo.PriceData.QuotaToPreConsume)

			perCallInfo := &relaycommon.RelayInfo{OriginModelName: "ratio-guard-model"}
			_, err = helper.ModelPriceHelperPerCall(c, perCallInfo)
			require.ErrorContains(t, err, "selected channel ratio is invalid")

			saveBillingConfig(t)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				billing_setting.PluginBillingExprOption: `{"ratio-guard::declared-model":"tier(\"a\", u(\"seconds\") * 0.05)"}`,
				"billing_setting.billing_mode":          `{}`,
				"billing_setting.billing_expr":          `{}`,
			}))
			plugin, err := pluginruntime.NewRegistry().Register(ratioGuardPlugin, pluginruntime.Options{})
			require.NoError(t, err)
			submitted := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				submitted++
				_, _ = w.Write([]byte(`{"id":"job"}`))
			}))
			defer server.Close()

			taskCtx, taskInfo := newTaskSubmitContext(t, "declared-model", "")
			taskCtx.Set("channel_ratio", ratio)
			taskCtx.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
			taskCtx.Set("task_plugin_key", plugin.Meta.Key)
			common.SetContextKey(taskCtx, constant.ContextKeyChannelBaseUrl, server.URL)
			taskInfo.OriginModelName = "declared-model"
			billing := &imageReservation{limit: 1 << 30}
			taskInfo.Billing = billing

			_, taskErr := RelayTaskSubmit(taskCtx, taskInfo)
			require.NotNil(t, taskErr, "the task expression branch must not price an invalid ratio as 0")
			assert.Equal(t, "model_price_error", taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.Zero(t, billing.held, "nothing may be reserved")
			assert.Zero(t, submitted, "the upstream must not see the request")
		})
	}
}
