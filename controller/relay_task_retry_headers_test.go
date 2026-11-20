package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskRetryIsolatesChannelHeadersAndPluginAttempt(t *testing.T) {
	withTaskRetries(t, 1)
	previousLog := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLog })
	events := []string{}
	db := setupTaskSubmissionDatabase(t, true, &events)
	first := model.Channel{Id: 101, Name: "primary", Key: "primary", Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Models: "plugin-model", ChannelRatio: "1", Priority: common.GetPointer(int64(10)), ParamOverride: common.GetPointer(`{"operations":[{"mode":"set_header","path":"X-Primary","value":"primary"}]}`)}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, first.AddAbilities(nil))
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("key", "backup").Error)
	plugin, err := jsplugin.DefaultRegistry.Register(`export const meta={apiVersion:1,key:"retry-probe",name:"Retry probe",version:"1.0.0",author:{name:"Test"}};
 export function egress(ctx) {
 if(ctx.channel.id===1 && ctx.attempt===1) return {action:"reject",code:"retry_observed",message:"Backup saw retry one"};
 return {action:"continue"};
 }`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key)) })
	previous, err := common.Marshal(request_plugin_setting.GetInstances())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: string(previous)}))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: `[{"id":"probe","name":"probe","plugin":"retry-probe","enabled":true,"global":true}]`}))
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events})
	info.LockedChannel = nil
	_, _, _, apiErr := requestplugin.SubmitIngress(c, "task", info.OriginModelName)
	require.Nil(t, apiErr)
	var channels []int
	_, taskErr := executeTaskSubmissionWith(c, info, func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		channels = append(channels, info.ChannelId)
		_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte("{}"), info)
		require.NoError(t, err)
		outbound := httptest.NewRequest(http.MethodPost, "/submit", nil)
		require.NoError(t, channel.ApplyHeaderOverride(info, c, outbound))
		if info.ChannelId == first.Id {
			require.Equal(t, "primary", outbound.Header.Get("X-Primary"))
			info.StreamStatus = relaycommon.NewStreamStatus()
			info.PerformanceOutputTokens = 7
			info.PerformanceBusinessRejection = true
			return nil, &dto.TaskError{Code: "plugin_busy", StatusCode: http.StatusTooManyRequests, LocalError: true}
		}
		assert.Empty(t, outbound.Header.Get("X-Primary"))
		assert.Nil(t, info.StreamStatus)
		assert.Zero(t, info.PerformanceOutputTokens)
		assert.False(t, info.PerformanceBusinessRejection)
		_, apiErr := requestplugin.TaskEgress(c, info, strings.NewReader("{}"))
		require.NotNil(t, apiErr, "backup plugin must observe the second attempt")
		require.Contains(t, apiErr.Error(), "retry_observed")
		return &relay.TaskSubmitResult{UpstreamTaskID: "retry-fixture", Platform: "plugin"}, nil
	})
	require.Nil(t, taskErr)
	require.Equal(t, []int{first.Id, 1}, channels)
}
