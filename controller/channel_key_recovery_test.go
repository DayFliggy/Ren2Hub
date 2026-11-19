package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// The scheduled channel test also tests automatically disabled keys of
// multi-key channels, each with one real request outside the rotation. A key
// that answers returns to the rotation; one that fails keeps its state and
// writes no routing health. Manually disabled keys and channels, keys whose
// text repeats in the channel, channels whose keys are provider plugin
// credentials and runs an administrator started are not touched.
func TestScheduledTestRecoversAutoDisabledKeys(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	require.NoError(t, model.DB.Model(fixture.user).Update("role", common.RoleRootUser).Error)
	previousEnable := common.AutomaticEnableChannelEnabled
	common.AutomaticEnableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = previousEnable })
	monitor := operation_setting.GetMonitorSetting()
	previousMode := monitor.ChannelTestMode
	t.Cleanup(func() { monitor.ChannelTestMode = previousMode })
	// Passive recovery health-tests only automatically disabled channels, so
	// the enabled channels below see nothing but the key recovery.
	monitor.ChannelTestMode = operation_setting.ChannelTestModePassiveRecovery

	var mu sync.Mutex
	hits, healthy := map[string]int{}, map[string]bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		hits[key]++
		ok := healthy[key]
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(upstream.Close)
	hitsOf := func(key string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[key]
	}
	totalHits := func() int {
		mu.Lock()
		defer mu.Unlock()
		sum := 0
		for _, n := range hits {
			sum += n
		}
		return sum
	}
	setHealthy := func(keys ...string) {
		mu.Lock()
		defer mu.Unlock()
		for _, key := range keys {
			healthy[key] = true
		}
	}
	mapping := `{"ws-billing":"gpt-4o"}`
	insert := func(t *testing.T, channel *model.Channel) *model.Channel {
		t.Helper()
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Enabled: channel.Status == common.ChannelStatusEnabled}).Error)
		t.Cleanup(func() {
			require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
			require.NoError(t, model.DB.Delete(channel).Error)
		})
		return channel
	}
	withKeys := func(t *testing.T, name string, keys []string, keyStatus map[int]int, status int) *model.Channel {
		t.Helper()
		return insert(t, &model.Channel{Name: name, Key: strings.Join(keys, "\n"), Status: status, Type: constant.ChannelTypeOpenAI, Models: "ws-billing",
			BaseURL: &upstream.URL, ModelMapping: &mapping, AutoBan: common.GetPointer(1),
			ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: len(keys), MultiKeyStatusList: keyStatus, MultiKeyMode: constant.MultiKeyModePolling}})
	}
	newChannel := func(t *testing.T, name string, size int, keyStatus map[int]int, status int) *model.Channel {
		t.Helper()
		keys := make([]string, size)
		for i := range keys {
			keys[i] = fmt.Sprintf("%s-k%d", name, i)
		}
		return withKeys(t, name, keys, keyStatus, status)
	}
	stored := func(t *testing.T, id int) model.Channel {
		t.Helper()
		var channel model.Channel
		require.NoError(t, model.DB.First(&channel, id).Error)
		return channel
	}
	round := func(t *testing.T) {
		t.Helper()
		_, err := runChannelTestTask(context.Background(), "", false, nil)
		require.NoError(t, err)
	}
	// recordRouteFailure records one failed relay attempt of the channel the
	// way the relay does, on the mapped model the breaker counts.
	recordRouteFailure := func(t *testing.T, channel *model.Channel) {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		service.BeginRouteObservation(c)
		common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
		c.Set("model_mapping", mapping)
		service.RequestPolicy(c).BeginAttempt(channel)
		info := &relaycommon.RelayInfo{OriginModelName: "ws-billing", RelayMode: relayconstant.RelayModeChatCompletions, StartTime: time.Now()}
		service.MarkRouteAttemptSent(c, info)
		service.FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))
	}
	breakerOpen := func(id int) bool {
		for _, row := range service.RoutingSnapshot().Breakers {
			if row.ChannelID == id {
				return true
			}
		}
		return false
	}
	auto, manual := common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled

	t.Run("a recovered key returns to the rotation", func(t *testing.T) {
		channel := newChannel(t, "recover", 5, map[int]int{2: auto}, common.ChannelStatusEnabled)
		setHealthy("recover-k2")
		round(t)
		require.Equal(t, 1, hitsOf("recover-k2"))
		for _, i := range []int{0, 1, 3, 4} {
			require.Zero(t, hitsOf(fmt.Sprintf("recover-k%d", i)), "enabled keys are not part of the recovery")
		}
		after := stored(t, channel.Id)
		require.Equal(t, common.ChannelStatusEnabled, after.Status)
		served := map[int]bool{}
		for range 5 {
			_, index, apiErr := after.GetNextEnabledKey()
			require.Nil(t, apiErr)
			served[index] = true
		}
		require.True(t, served[2], "polling hands out the recovered key again")
	})

	t.Run("a failing key keeps its state and writes no route health", func(t *testing.T) {
		channel := newChannel(t, "failing", 5, map[int]int{2: auto}, common.ChannelStatusEnabled)
		for range routeBreakerOpenFailuresForTest - 1 {
			recordRouteFailure(t, channel)
		}
		require.False(t, breakerOpen(channel.Id))
		for want := 1; want <= 2; want++ {
			round(t)
			require.Equal(t, want, hitsOf("failing-k2"), "at most one upstream request per round")
		}
		after := stored(t, channel.Id)
		require.Equal(t, common.ChannelStatusEnabled, after.Status)
		require.Equal(t, auto, after.ChannelInfo.MultiKeyStatusList[2])
		require.False(t, breakerOpen(channel.Id), "two failed recovery tests added no breaker sample to the four failures")
		recordRouteFailure(t, channel)
		require.True(t, breakerOpen(channel.Id), "the fifth relay failure opens it, so the four were counted")
	})

	t.Run("a manually disabled key is never tested", func(t *testing.T) {
		channel := newChannel(t, "manual", 3, map[int]int{1: manual}, common.ChannelStatusEnabled)
		setHealthy("manual-k1")
		round(t)
		require.Zero(t, hitsOf("manual-k1"))
		require.Equal(t, manual, stored(t, channel.Id).ChannelInfo.MultiKeyStatusList[1])
	})

	// The status write matches a key by its text, so the slot of a key whose
	// text repeats in the channel is not certain; such keys are left alone.
	t.Run("a key whose text repeats is never tested", func(t *testing.T) {
		channel := withKeys(t, "duplicate", []string{"duplicate-k", "duplicate-k", "duplicate-other"}, map[int]int{0: manual, 1: auto}, common.ChannelStatusEnabled)
		setHealthy("duplicate-k")
		round(t)
		require.Zero(t, hitsOf("duplicate-k"))
		after := stored(t, channel.Id)
		require.Equal(t, manual, after.ChannelInfo.MultiKeyStatusList[0])
		require.Equal(t, auto, after.ChannelInfo.MultiKeyStatusList[1])
	})

	// A provider plugin may send with another slot's credential once the
	// channel is narrowed to one key, so its keys stay with the plugin's own
	// scheduled hook.
	t.Run("provider plugin credentials are never tested", func(t *testing.T) {
		const pluginKey = "key-recovery-plugin"
		_, err := jsplugin.DefaultRegistry.Register(strings.ReplaceAll(loginPluginSource, "%KEY%", pluginKey), jsplugin.Options{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = jsplugin.DefaultRegistry.Unregister(pluginKey) })
		lines := []string{pluginCredentialLine(t, pluginKey, "plugin-a"), pluginCredentialLine(t, pluginKey, "plugin-b")}
		setHealthy("plugin-a", "plugin-b")
		channel := &model.Channel{Name: "plugin-keys", Key: strings.Join(lines, "\n"), Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeTaskPlugin,
			Models: "ws-billing", BaseURL: &upstream.URL, AutoBan: common.GetPointer(1),
			ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyStatusList: map[int]int{1: auto}, MultiKeyMode: constant.MultiKeyModePolling}}
		channel.SetSetting(dto.ChannelSettings{TaskPluginKey: pluginKey})
		insert(t, channel)
		before := totalHits()
		round(t)
		require.Equal(t, before, totalHits(), "nothing is sent for a plugin channel's keys")
		require.Equal(t, auto, stored(t, channel.Id).ChannelInfo.MultiKeyStatusList[1])
	})

	t.Run("a channel disabled by hand after planning is not tested", func(t *testing.T) {
		channel := newChannel(t, "late-manual", 3, map[int]int{0: auto}, common.ChannelStatusEnabled)
		setHealthy("late-manual-k0")
		snapshot := stored(t, channel.Id)
		plans := keyRecoveryPlans([]*model.Channel{&snapshot})
		require.Len(t, plans[channel.Id], 1)
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("status", manual).Error)
		testUserID, err := resolveChannelTestUserID(nil)
		require.NoError(t, err)
		recoverChannelKeys(context.Background(), &snapshot, plans[channel.Id], testUserID)
		require.Zero(t, hitsOf("late-manual-k0"))
		after := stored(t, channel.Id)
		require.Equal(t, manual, after.Status)
		require.Equal(t, auto, after.ChannelInfo.MultiKeyStatusList[0])
	})

	t.Run("a channel with every key disabled comes back", func(t *testing.T) {
		channel := newChannel(t, "exhausted", 3, map[int]int{0: auto, 1: auto, 2: auto}, auto)
		setHealthy("exhausted-k1")
		round(t)
		after := stored(t, channel.Id)
		require.Equal(t, common.ChannelStatusEnabled, after.Status)
		require.NotContains(t, after.ChannelInfo.MultiKeyStatusList, 1)
		var ability model.Ability
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
		require.True(t, ability.Enabled)
	})

	t.Run("each round tests at most three keys and reaches every key", func(t *testing.T) {
		newChannel(t, "capped", 5, map[int]int{0: auto, 1: auto, 2: auto, 3: auto, 4: auto}, auto)
		total := func() int {
			sum := 0
			for i := range 5 {
				sum += hitsOf(fmt.Sprintf("capped-k%d", i))
			}
			return sum
		}
		round(t)
		require.Equal(t, 3, total())
		round(t)
		require.Equal(t, 6, total())
		for i := range 5 {
			require.Positive(t, hitsOf(fmt.Sprintf("capped-k%d", i)), "the keys tested longest ago go first")
		}
	})

	// Only a run the scheduler started recovers keys, in every configured
	// mode; "test all channels" by an administrator does not.
	t.Run("only scheduled runs recover", func(t *testing.T) {
		for _, tc := range []struct {
			name, configured, requested string
			want                        int
		}{
			{"scheduled all", operation_setting.ChannelTestModeScheduledAll, "", 1},
			{"scheduled auto ban only", operation_setting.ChannelTestModeAutoBanOnly, "", 1},
			{"scheduled passive recovery", operation_setting.ChannelTestModePassiveRecovery, "", 1},
			{"manual test all", operation_setting.ChannelTestModePassiveRecovery, operation_setting.ChannelTestModeScheduledAll, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				name := "entry-" + strings.ReplaceAll(tc.name, " ", "-")
				channel := newChannel(t, name, 2, map[int]int{1: auto}, common.ChannelStatusEnabled)
				setHealthy(name+"-k0", name+"-k1")
				monitor.ChannelTestMode = tc.configured
				t.Cleanup(func() { monitor.ChannelTestMode = operation_setting.ChannelTestModePassiveRecovery })
				_, err := runChannelTestTask(context.Background(), tc.requested, false, nil)
				require.NoError(t, err)
				require.Equal(t, tc.want, hitsOf(name+"-k1"))
				recovered := stored(t, channel.Id).ChannelInfo.MultiKeyStatusList[1] != auto
				require.Equal(t, tc.want == 1, recovered)
			})
		}
	})

	t.Run("nothing is sent while automatic enabling is off", func(t *testing.T) {
		common.AutomaticEnableChannelEnabled = false
		t.Cleanup(func() { common.AutomaticEnableChannelEnabled = true })
		channel := newChannel(t, "switched-off", 3, map[int]int{0: auto}, common.ChannelStatusEnabled)
		setHealthy("switched-off-k0")
		round(t)
		require.Zero(t, hitsOf("switched-off-k0"))
		require.Equal(t, auto, stored(t, channel.Id).ChannelInfo.MultiKeyStatusList[0])
	})
}

// routeBreakerOpenFailuresForTest is the consecutive failure count that opens
// a breaker (service.routeBreakerOpenFailures).
const routeBreakerOpenFailuresForTest = 5
