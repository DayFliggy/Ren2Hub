package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestRelayRetryAfter429DemotesChannelForNextRequest(t *testing.T) {
	testRelay429Cooldown(t, "60")
}

func TestRelayHeaderless429DemotesChannelForNextRequest(t *testing.T) {
	testRelay429Cooldown(t, "")
}

func testRelay429Cooldown(t *testing.T, retryAfter string) {
	t.Helper()
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	oldRetries := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = oldRetries })

	var primaryHits, backupHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer upstream-") {
			primaryHits.Add(1)
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`)
			return
		}
		backupHits.Add(1)
		_, _ = fmt.Fprint(w, `{"id":"completed","status":"completed","usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}`)
	}))
	t.Cleanup(upstream.Close)

	// The primary has weight and the backup does not, so without a cooldown the
	// primary is always ordered first.
	weight := uint(10)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.channel.Id).Updates(map[string]any{"base_url": upstream.URL, "weight": weight}).Error)
	// Fixtures reuse database IDs while route health lives for the process.
	require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id))
	t.Cleanup(func() { require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id)) })
	backup := *fixture.channel
	backup.Id = 0
	backup.Name = "responses-http-cooldown-backup"
	backup.Key = "backup-key"
	backup.BaseURL = &upstream.URL
	require.NoError(t, model.DB.Create(&backup).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: backup.Id, Model: "ws-billing", Enabled: true}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", backup.Id).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Delete(&backup).Error)
	})

	status, body := postResponsesThroughGateway(t, fixture)
	waitRelayFinished(t, fixture)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, int64(1), primaryHits.Load(), "the weighted primary is tried first")
	require.Equal(t, int64(1), backupHits.Load(), "the retry reaches the backup")

	status, body = postResponsesThroughGateway(t, fixture)
	waitRelayFinished(t, fixture)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, int64(1), primaryHits.Load(), "the cooling primary must not be tried first again")
	require.Equal(t, int64(2), backupHits.Load())

	require.NoError(t, model.DB.Model(&backup).Update("status", common.ChannelStatusManuallyDisabled).Error)
	status, body = postResponsesThroughGateway(t, fixture)
	waitRelayFinished(t, fixture)
	require.Equal(t, http.StatusTooManyRequests, status, body)
	require.Equal(t, int64(2), primaryHits.Load(), "the cooling primary still serves when it is the only candidate")
	require.Equal(t, int64(2), backupHits.Load())

	var primary model.Channel
	require.NoError(t, model.DB.First(&primary, fixture.channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, primary.Status, "demotion must not come from disabling the channel")
}
