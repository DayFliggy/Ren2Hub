package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func postResponsesWithSession(t *testing.T, fixture *responsesWSBillingTest, session string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Session", session)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode, string(body)
}

// A strict session bound to a channel whose breaker is open is refused at
// selection, before pre-consumption and without reaching the upstream. The
// binding is cleared, so the session's next request routes normally and binds
// the channel that served it.
func TestStrictAffinityOpenBreakerRejectsBeforeBilling(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	oldRetries := common.RetryTimes
	common.RetryTimes = 0
	t.Cleanup(func() { common.RetryTimes = oldRetries })
	setting := operation_setting.GetChannelAffinitySetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous; service.ClearChannelAffinityCacheAll() })
	rules, err := common.Marshal([]operation_setting.ChannelAffinityRule{{
		Name: "strict-session", ModelRegex: []string{"^ws-billing$"}, SessionMode: "strict",
		KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}},
	}})
	require.NoError(t, err)
	snapshot, err := model.BuildRequestPolicy(map[string]string{"channel_affinity_setting.enabled": "true", "channel_affinity_setting.session_mode": "strict", "channel_affinity_setting.rules": string(rules)})
	require.NoError(t, err)
	*setting = snapshot.Affinity

	var primaryHits, backupHits atomic.Int64
	var primaryFails atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer upstream-") {
			primaryHits.Add(1)
			if primaryFails.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, `{"error":{"type":"server_error","code":"server_error","message":"upstream exploded"}}`)
				return
			}
		} else {
			backupHits.Add(1)
		}
		_, _ = fmt.Fprint(w, `{"id":"completed","status":"completed","usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}`)
	}))
	t.Cleanup(upstream.Close)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.channel.Id).Updates(map[string]any{"base_url": upstream.URL, "priority": 10}).Error)
	require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id))
	t.Cleanup(func() { require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id)) })
	backup := *fixture.channel
	backup.Id = 0
	backup.Name = "strict-affinity-backup"
	backup.Key = "backup-key"
	backup.ChannelInfo = model.ChannelInfo{}
	backup.BaseURL = &upstream.URL
	backup.Priority = common.GetPointer[int64](0)
	require.NoError(t, model.DB.Create(&backup).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: backup.Id, Model: "ws-billing", Enabled: true}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", backup.Id).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Delete(&backup).Error)
	})
	session := "strict-" + t.Name()
	send := func() (int, string) {
		t.Helper()
		status, body := postResponsesWithSession(t, fixture, session)
		waitRelayFinished(t, fixture)
		return status, body
	}

	status, body := send()
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, int64(1), primaryHits.Load(), "the higher priority primary serves and is bound")
	primaryFails.Store(true)
	for range 5 {
		status, body = send()
		require.Equal(t, http.StatusInternalServerError, status, body)
	}
	require.Equal(t, int64(6), primaryHits.Load())
	require.Zero(t, backupHits.Load(), "strict never leaves the bound channel")

	var before model.User
	require.NoError(t, model.DB.First(&before, fixture.user.Id).Error)
	var tokenBefore model.Token
	require.NoError(t, model.DB.First(&tokenBefore, fixture.token.Id).Error)
	// Distribute refuses before the relay handler, which therefore never runs.
	status, body = postResponsesWithSession(t, fixture, session)
	require.Equal(t, http.StatusServiceUnavailable, status, body)
	require.Contains(t, body, "strict_session_binding_unavailable")
	require.Equal(t, int64(6), primaryHits.Load(), "the open channel receives nothing")
	require.Zero(t, backupHits.Load())
	var after model.User
	require.NoError(t, model.DB.First(&after, fixture.user.Id).Error)
	var tokenAfter model.Token
	require.NoError(t, model.DB.First(&tokenAfter, fixture.token.Id).Error)
	require.Equal(t, before.Quota, after.Quota, "no pre-consumption")
	require.Equal(t, tokenBefore.RemainQuota, tokenAfter.RemainQuota)

	status, body = send()
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, int64(1), backupHits.Load(), "the cleared session routes normally to the healthy backup")

	// Clearing the primary's breaker shows the session is now bound to the
	// backup: the higher priority primary would win otherwise.
	primaryFails.Store(false)
	require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id))
	status, body = send()
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, int64(2), backupHits.Load())
	require.Equal(t, int64(6), primaryHits.Load())
}
