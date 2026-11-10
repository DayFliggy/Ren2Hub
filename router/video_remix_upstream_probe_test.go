//go:build upstream_probe

package router

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These desired-behavior probes remain red while the shared NewAPI defects
// are pending. They are opt-in, never counted as default regression passes.
// The source ledger records their upstream status and blocked assertions.

// A known-upstream result requires the exact panic family and originating
// function. Unrelated 500s fail the normal response assertions below.
func requireRemixPastOriginResolution(t *testing.T, route *remixRoute, response *httptest.ResponseRecorder) {
	t.Helper()
	if route.panicValue == nil {
		return
	}
	require.Equal(t, http.StatusInternalServerError, response.Code)
	panicErr, ok := route.panicValue.(runtime.Error)
	require.True(t, ok, "unexpected non-runtime panic: %T", route.panicValue)
	require.Contains(t, panicErr.Error(), "invalid memory address or nil pointer dereference")
	require.Contains(t, string(route.panicStack), "relay.ResolveOriginTask(")
	require.Contains(t, string(route.panicStack), "/relay/relay_task.go:")
	require.Zero(t, route.upstream.sends)
	frame := ""
	lines := strings.Split(string(route.panicStack), "\n")
	for index, line := range lines {
		if strings.Contains(line, "relay.ResolveOriginTask(") && index+1 < len(lines) {
			if fields := strings.Fields(lines[index+1]); len(fields) > 0 {
				frame = fields[0]
			}
			break
		}
	}
	require.NotEmpty(t, frame)
	t.Logf("upstream_panic type=%T message=%q function=relay.ResolveOriginTask frame=%s", panicErr, panicErr.Error(), frame)
	t.Fatalf("known_upstream=N2; blocker=nil_ChannelMeta_in_ResolveOriginTask; blocked=outbound_id,plugin_action,context_mapping,platform,billing,retry")
}

func TestUpstreamNativeRemixRouteSendsUpstreamRemixWithOriginChannelContext(t *testing.T) {
	route := newRemixRoute(t)
	mapping := `{"sora-2":"sora-2-pro"}`
	headers := `{"X-Remix-Probe":"origin-channel"}`
	channel := route.channel(t, model.Channel{
		Type: constant.ChannelTypeSora, Name: "sora", Models: "sora-2", Key: "key-one\nkey-two",
		ModelMapping: &mapping, HeaderOverride: &headers,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeyMode: constant.MultiKeyModePolling},
	})
	route.task(t, soraOriginTask(channel.Id))
	before := remixUserQuota(t)

	recorder := route.remix(t, route.token(t, "remixplain", ""), "task_origin_public")
	requireRemixPastOriginResolution(t, route, recorder)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, 1, route.upstream.sends)
	assert.Equal(t, "/v1/videos/video_origin_up/remix", route.upstream.path, "the vendor receives its own video id")
	assert.Contains(t, route.upstream.body, `"model":"sora-2-pro"`, "the origin channel's model mapping applies")
	assert.Equal(t, "origin-channel", route.upstream.header.Get("X-Remix-Probe"), "the origin channel's header override applies")
	assert.Equal(t, "Bearer key-one", route.upstream.header.Get("Authorization"), "one remix selects one key")
	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex, "the polling cursor advances once")
	assert.Equal(t, 600_000, before-remixUserQuota(t), "0.1 x 500000 x the inherited 12 seconds")
	var task model.Task
	require.NoError(t, model.DB.Where("action = ?", constant.TaskActionRemix).First(&task).Error)
	assert.Equal(t, "video_remix_up", task.PrivateData.UpstreamTaskID)
	assert.Equal(t, map[string]float64{"seconds": 12}, task.PrivateData.BillingContext.OtherRatios)
}

func TestUpstreamNativeRemixRouteLegacyTaskUsesPublicIDAndDataRatios(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeSora} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			route := newRemixRoute(t)
			channel := route.channel(t, model.Channel{Type: channelType, Name: "legacy", Models: "sora-2"})
			legacy := model.Task{TaskID: "video_legacy", Platform: constant.TaskPlatform(strconv.Itoa(channelType)), ChannelId: channel.Id}
			legacy.SetData(map[string]any{"model": "sora-2", "seconds": "8", "size": "1792x1024"})
			route.task(t, legacy)
			before := remixUserQuota(t)

			recorder := route.remix(t, route.token(t, "remixlegacy", ""), "video_legacy")
			requireRemixPastOriginResolution(t, route, recorder)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.Equal(t, "/v1/videos/video_legacy/remix", route.upstream.path, "a task without an upstream id keeps its own id")
			assert.Equal(t, 666_666, before-remixUserQuota(t), "0.1 x 500000 x 8 seconds x 1.666667 size, truncated")
		})
	}
}

func TestUpstreamNativeRemixRouteOnTaskPluginChannel(t *testing.T) {
	route := newRemixRoute(t)
	setting := `{"task_plugin_key":"sora"}`
	channel := route.channel(t, model.Channel{Type: constant.ChannelTypeTaskPlugin, Name: "sora-plugin", Models: "sora-2", Setting: &setting})
	route.task(t, soraOriginTask(channel.Id))

	recorder := route.remix(t, route.token(t, "remixplugin", ""), "task_origin_public")
	requireRemixPastOriginResolution(t, route, recorder)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "/v1/videos/video_origin_up/remix", route.upstream.path)
}

func TestUpstreamNativeRemixRouteChecksKeyModelLimitAgainstOriginModel(t *testing.T) {
	route := newRemixRoute(t)
	channel := route.channel(t, model.Channel{Type: constant.ChannelTypeSora, Name: "sora", Models: "sora-2"})
	route.task(t, soraOriginTask(channel.Id))

	allowed := route.remix(t, route.token(t, "remixallowed", "sora-2"), "task_origin_public")
	requireRemixPastOriginResolution(t, route, allowed)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	require.Equal(t, 1, route.upstream.sends)

	before := remixUserQuota(t)
	denied := route.remix(t, route.token(t, "remixdenied", "gpt-4o"), "task_origin_public")
	requireRemixPastOriginResolution(t, route, denied)
	assert.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	assert.Equal(t, 1, route.upstream.sends, "a denied model is never sent")
	assert.Equal(t, before, remixUserQuota(t))
}

func TestUpstreamNativeRemixRouteRejectsOriginChannelOfAnotherPlatform(t *testing.T) {
	klingSetting := `{"task_plugin_key":"kling"}`
	for name, tc := range map[string]struct {
		channel  model.Channel
		platform constant.TaskPlatform
	}{
		"kling task on a channel now typed openai": {
			channel:  model.Channel{Type: constant.ChannelTypeOpenAI, Name: "openai", Models: "sora-2"},
			platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeKling)),
		},
		"sora task on a plugin channel rebound to kling": {
			channel:  model.Channel{Type: constant.ChannelTypeTaskPlugin, Name: "rebound", Models: "sora-2", Setting: &klingSetting},
			platform: constant.TaskPlatform("sora"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			route := newRemixRoute(t)
			channel := route.channel(t, tc.channel)
			task := soraOriginTask(channel.Id)
			task.Platform = tc.platform
			route.task(t, task)
			tasks, before := remixTaskCount(t), remixUserQuota(t)

			recorder := route.remix(t, route.token(t, "remixmismatch", ""), "task_origin_public")
			requireRemixPastOriginResolution(t, route, recorder)

			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "origin_task_platform_mismatch")
			assert.Zero(t, route.upstream.sends)
			assert.Equal(t, before, remixUserQuota(t), "nothing is pre-charged")
			assert.Equal(t, tasks, remixTaskCount(t))
		})
	}
}

func TestUpstreamNativeRemixRouteRetryDoesNotCompoundInheritedRatios(t *testing.T) {
	previousRetries := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = previousRetries })
	route := newRemixRoute(t)
	autoBanOff := 0
	channel := route.channel(t, model.Channel{Type: constant.ChannelTypeSora, Name: "sora", Models: "sora-2", AutoBan: &autoBanOff})
	route.task(t, soraOriginTask(channel.Id))
	route.upstream.failures = 1
	before := remixUserQuota(t)

	recorder := route.remix(t, route.token(t, "remixretry", ""), "task_origin_public")
	requireRemixPastOriginResolution(t, route, recorder)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, 2, route.upstream.sends)
	assert.Equal(t, "/v1/videos/video_origin_up/remix", route.upstream.path)
	assert.Equal(t, 600_000, before-remixUserQuota(t))
}
