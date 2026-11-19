package controller

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// A valid param override condition that meets a request value of another
// type is the request's fault (upstream #8345). Through the real entry with
// two candidates, retries and automatic disabling on, the request selects one
// channel, gets 400, reaches no upstream and disables nothing. The request
// value is legal: "user" may be any string; the rule compares it with a number.
func TestParamOverrideRequestValueErrorStaysOnOneChannel(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	oldRetries, oldDisable, oldErrorLog := common.RetryTimes, common.AutomaticDisableChannelEnabled, constant.ErrorLogEnabled
	common.RetryTimes, common.AutomaticDisableChannelEnabled, constant.ErrorLogEnabled = 2, true, true
	t.Cleanup(func() {
		common.RetryTimes, common.AutomaticDisableChannelEnabled, constant.ErrorLogEnabled = oldRetries, oldDisable, oldErrorLog
	})
	var hits atomic.Int64
	fixture.httpUpstream = func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ok","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}
	override := `{"operations":[{"path":"max_output_tokens","mode":"set","value":512,"conditions":[{"path":"user","mode":"gt","value":1}]}]}`
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", fixture.channel.Id).Updates(map[string]any{"param_override": override, "auto_ban": 1}).Error)
	require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id))
	t.Cleanup(func() { require.NoError(t, model.ChannelRoutingChanged(fixture.channel.Id)) })
	var primary model.Channel
	require.NoError(t, model.DB.First(&primary, fixture.channel.Id).Error)
	backup := primary
	backup.Id = 0
	backup.Name = "param-override-backup"
	backup.Key = "backup-key"
	require.NoError(t, model.DB.Create(&backup).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: backup.Id, Model: "ws-billing", Enabled: true}).Error)
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("channel_id = ?", backup.Id).Delete(&model.Ability{}).Error)
		require.NoError(t, model.DB.Delete(&backup).Error)
	})

	request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello","user":"alice"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	waitRelayFinished(t, fixture)

	require.Equal(t, http.StatusBadRequest, response.StatusCode, string(body))
	require.Contains(t, string(body), "invalid_request")
	require.Zero(t, hits.Load(), "no upstream receives the request")
	for _, id := range []int{primary.Id, backup.Id} {
		var stored model.Channel
		require.NoError(t, model.DB.First(&stored, id).Error)
		require.Equal(t, common.ChannelStatusEnabled, stored.Status, "channel %d stays enabled", id)
	}
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", "responses-http-billing", model.LogTypeError).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, 1, strings.Count(logs[0].Other, `"round":`), "one selection round, no retry: %s", logs[0].Other)
}
