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
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// A channel holds one key, so an automatically disabled key is an
// automatically disabled channel. The native scheduled channel test brings
// back one whose test now succeeds and leaves one that still fails disabled.
func TestScheduledTestReenablesAutoDisabledChannel(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	require.NoError(t, model.DB.Model(fixture.user).Update("role", common.RoleRootUser).Error)
	previousEnable := common.AutomaticEnableChannelEnabled
	common.AutomaticEnableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = previousEnable })
	monitor := operation_setting.GetMonitorSetting()
	previousMode := monitor.ChannelTestMode
	t.Cleanup(func() { monitor.ChannelTestMode = previousMode })
	monitor.ChannelTestMode = operation_setting.ChannelTestModePassiveRecovery

	var mu sync.Mutex
	hits := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		hits[key]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if key != "recovered-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(upstream.Close)
	mapping := `{"ws-billing":"gpt-4o"}`
	insert := func(name, key string) *model.Channel {
		channel := &model.Channel{Name: name, Key: key, Status: common.ChannelStatusAutoDisabled, Type: constant.ChannelTypeOpenAI,
			Models: "ws-billing", BaseURL: &upstream.URL, ModelMapping: &mapping, AutoBan: common.GetPointer(1), ChannelRatio: "1"}
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{ChannelId: channel.Id, Model: "ws-billing", Enabled: false}).Error)
		t.Cleanup(func() {
			require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
			require.NoError(t, model.DB.Delete(channel).Error)
		})
		return channel
	}
	recovered := insert("recovered", "recovered-key")
	failing := insert("still-failing", "rejected-key")

	_, err := runChannelTestTask(context.Background(), "", false, nil)
	require.NoError(t, err)

	mu.Lock()
	require.Equal(t, 1, hits["recovered-key"])
	require.Equal(t, 1, hits["rejected-key"])
	mu.Unlock()
	statusOf := func(id int) (int, bool) {
		var channel model.Channel
		require.NoError(t, model.DB.First(&channel, id).Error)
		var ability model.Ability
		require.NoError(t, model.DB.Where("channel_id = ?", id).First(&ability).Error)
		return channel.Status, ability.Enabled
	}
	status, enabled := statusOf(recovered.Id)
	require.Equal(t, common.ChannelStatusEnabled, status)
	require.True(t, enabled, "the recovered channel serves again")
	status, enabled = statusOf(failing.Id)
	require.Equal(t, common.ChannelStatusAutoDisabled, status)
	require.False(t, enabled)
}
