package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// The scheduled channel test also tests automatically disabled keys of
// multi-key channels, each with one real request outside the rotation. A key
// that answers returns to the rotation; one that fails keeps its state and
// writes no routing health. Manually disabled keys and channels are not touched.
func TestScheduledTestRecoversAutoDisabledKeys(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	require.NoError(t, model.DB.Model(fixture.user).Update("role", common.RoleRootUser).Error)
	previousEnable := common.AutomaticEnableChannelEnabled
	common.AutomaticEnableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = previousEnable })

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
	setHealthy := func(key string) {
		mu.Lock()
		defer mu.Unlock()
		healthy[key] = true
	}
	newChannel := func(t *testing.T, name string, size int, keyStatus map[int]int, status int) *model.Channel {
		t.Helper()
		keys := make([]string, size)
		for i := range keys {
			keys[i] = fmt.Sprintf("%s-k%d", name, i)
		}
		mapping := `{"ws-billing":"gpt-4o"}`
		channel := &model.Channel{Name: name, Key: strings.Join(keys, "\n"), Status: status, Type: constant.ChannelTypeOpenAI, Models: "ws-billing",
			BaseURL: &upstream.URL, ModelMapping: &mapping, AutoBan: common.GetPointer(1),
			ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: size, MultiKeyStatusList: keyStatus, MultiKeyMode: constant.MultiKeyModePolling}}
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Enabled: status == common.ChannelStatusEnabled}).Error)
		t.Cleanup(func() {
			require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
			require.NoError(t, model.DB.Delete(channel).Error)
		})
		return channel
	}
	stored := func(t *testing.T, id int) model.Channel {
		t.Helper()
		var channel model.Channel
		require.NoError(t, model.DB.First(&channel, id).Error)
		return channel
	}
	// Passive recovery health-tests only automatically disabled channels, so
	// the enabled channels below see nothing but the key recovery.
	round := func(t *testing.T) {
		t.Helper()
		_, err := runChannelTestTask(context.Background(), operation_setting.ChannelTestModePassiveRecovery, false, nil)
		require.NoError(t, err)
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
		require.NotContains(t, after.ChannelInfo.MultiKeyStatusList, 2)
	})

	t.Run("a failing key keeps its state and writes no route health", func(t *testing.T) {
		channel := newChannel(t, "failing", 5, map[int]int{2: auto}, common.ChannelStatusEnabled)
		for want := 1; want <= 2; want++ {
			round(t)
			require.Equal(t, want, hitsOf("failing-k2"), "at most one upstream request per round")
		}
		after := stored(t, channel.Id)
		require.Equal(t, common.ChannelStatusEnabled, after.Status)
		require.Equal(t, auto, after.ChannelInfo.MultiKeyStatusList[2])
		snapshot := service.RoutingSnapshot()
		for _, row := range snapshot.Breakers {
			require.NotEqual(t, channel.Id, row.ChannelID)
		}
		for _, row := range snapshot.Quality {
			require.NotEqual(t, channel.Id, row.ChannelID)
		}
		for _, row := range snapshot.Observations {
			require.NotEqual(t, channel.Id, row.ChannelID)
		}
	})

	t.Run("a manually disabled key is never tested", func(t *testing.T) {
		channel := newChannel(t, "manual", 3, map[int]int{1: manual}, common.ChannelStatusEnabled)
		setHealthy("manual-k1")
		round(t)
		require.Zero(t, hitsOf("manual-k1"))
		require.Equal(t, manual, stored(t, channel.Id).ChannelInfo.MultiKeyStatusList[1])
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
