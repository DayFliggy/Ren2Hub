//go:build known_upstream_restored

package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestRestoredProbeRelayFirstAttemptRejectsChannelWithoutEnabledKey(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	var attempts atomic.Int64
	fixture.httpUpstream = func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}
	info := fixture.channel.ChannelInfo
	info.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled}
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.channel.Id).Update("channel_info", info).Error)
	status, body := postResponsesThroughGateway(t, fixture)
	require.Zero(t, attempts.Load(), "a channel without an enabled key must not be sent")
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, body, "channel:no_available_key")
}
