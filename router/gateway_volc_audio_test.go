package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGatewayVolcSpeech(t *testing.T, handler http.HandlerFunc) (string, string) {
	t.Helper()
	url, key, _ := setupGatewaySDKWithUpstream(t, handler)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"tts-volc":0.001}`))
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	baseURL := "ws" + strings.TrimPrefix(*channel.BaseURL, "http")
	channel.Type, channel.Models, channel.Key, channel.BaseURL = constant.ChannelTypeVolcEngine, "tts-volc", "local-app|local-token", &baseURL
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "key": channel.Key, "base_url": baseURL}).Error)
	require.NoError(t, channel.AddAbilities(model.DB))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "remain_quota": 2000}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	return url, key
}

func gatewayVolcAudioFrame(t *testing.T, sequence int32) []byte {
	t.Helper()
	flag := volcengine.MsgTypeFlagPositiveSeq
	if sequence < 0 {
		flag = volcengine.MsgTypeFlagNegativeSeq
	}
	message, err := volcengine.NewMessage(volcengine.MsgTypeAudioOnlyServer, flag)
	require.NoError(t, err)
	message.Sequence, message.Payload = sequence, []byte{0, 1, 2, 3}
	frame, err := message.Marshal()
	require.NoError(t, err)
	return frame
}

func assertGatewayVolcSpeechAccounting(t *testing.T, charge, tokenQuota int, healthFailure bool) {
	t.Helper()
	awaitRealtimeLeasesReleased(t)
	var user model.User
	var token model.Token
	require.Eventually(t, func() bool {
		return model.DB.First(&user, 1).Error == nil && model.DB.First(&token, 1).Error == nil && user.Quota == 10000000-charge && user.UsedQuota == charge && token.RemainQuota == tokenQuota-charge
	}, time.Second, 10*time.Millisecond)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	if charge == 0 {
		assert.Empty(t, logs)
		assert.Zero(t, user.RequestCount)
	} else {
		require.Len(t, logs, 1)
		assert.Equal(t, 1, user.RequestCount)
		assert.Equal(t, 1, logs[0].ChannelId)
		assert.Equal(t, charge, logs[0].Quota)
		assert.Equal(t, 5, logs[0].PromptTokens)
		assert.Zero(t, logs[0].CompletionTokens)
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
		assert.Equal(t, "estimated", other["usage_source"])
		streamStatus, ok := other["stream_status"].(map[string]any)
		require.True(t, ok, "binary audio terminal status must also be persisted")
		if healthFailure {
			assert.Equal(t, "error", streamStatus["status"])
		} else {
			assert.Equal(t, "ok", streamStatus["status"])
			assert.Equal(t, "done", streamStatus["end_reason"])
		}
	}
	var failed []model.ChannelHealth
	require.NoError(t, model.DB.Where("failure_count > 0").Find(&failed).Error)
	assert.Equal(t, healthFailure, len(failed) > 0)
	for _, health := range failed {
		assert.Equal(t, 1, health.ChannelID)
		assert.Equal(t, 1, health.FailureCount, "one failed provider send must not be counted twice in a health scope")
	}
	var pending int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("status IN ?", []string{model.BillingRecoveryStatusActive, model.BillingRecoveryStatusSettlementPending, model.BillingRecoveryStatusRefundPending}).Count(&pending).Error)
	assert.Zero(t, pending)
}

func TestGatewayVolcSpeechWebSocketAccounting(t *testing.T) {
	for _, scenario := range []string{"completed", "interrupted", "truncated"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sse=%t", scenario, stream), func(t *testing.T) {
				frame := gatewayVolcAudioFrame(t, -1)
				if scenario == "interrupted" {
					frame = gatewayVolcAudioFrame(t, 1)
				} else if scenario == "truncated" {
					frame = frame[:len(frame)-1]
				}
				var sends atomic.Int32
				serverDone := make(chan error, 1)
				url, key := setupGatewayVolcSpeech(t, func(w http.ResponseWriter, request *http.Request) {
					sends.Add(1)
					assert.Equal(t, "/v1/audio/speech", request.URL.Path)
					assert.Equal(t, "Bearer;local-token", request.Header.Get("Authorization"))
					var user model.User
					if assert.NoError(t, model.DB.First(&user, 1).Error) {
						assert.Equal(t, 9999500, user.Quota, "speech precharge precedes the WebSocket handshake")
					}
					for _, resource := range []string{service.UserRouteLeaseKey(1), service.ChannelRouteLeaseKey(1)} {
						count, err := common.RDB.ZCard(context.Background(), resource).Result()
						assert.NoError(t, err)
						assert.EqualValues(t, 1, count)
					}
					conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
					if err != nil {
						serverDone <- err
						return
					}
					defer conn.Close()
					message, err := volcengine.ReceiveMessage(conn)
					if err != nil {
						serverDone <- err
						return
					}
					var speech volcengine.VolcengineTTSRequest
					if assert.NoError(t, common.Unmarshal(message.Payload, &speech)) {
						assert.Equal(t, "tts-volc", speech.Request.Model)
						assert.Equal(t, "hello", speech.Request.Text)
					}
					if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
						serverDone <- err
						return
					}
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
					serverDone <- nil
				})
				streamField := ""
				if stream {
					streamField = `,"stream_format":"sse"`
				}
				request, err := http.NewRequest(http.MethodPost, url+"/v1/audio/speech", strings.NewReader(`{"model":"tts-volc","input":"hello","voice":"alloy","response_format":"pcm"`+streamField+`}`))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+key)
				response, err := http.DefaultClient.Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				output, readErr := io.ReadAll(response.Body)
				select {
				case err := <-serverDone:
					require.NoError(t, err)
				case <-time.After(3 * time.Second):
					t.Fatalf("upstream WebSocket did not complete; gateway status=%d body=%s", response.StatusCode, output)
				}
				if scenario == "interrupted" && !stream {
					require.Error(t, readErr, "partial binary audio must not end as a complete HTTP response")
				} else {
					require.NoError(t, readErr)
				}
				assert.EqualValues(t, 1, sends.Load())
				charge := 500
				if scenario == "truncated" {
					charge = 0
					assert.Equal(t, http.StatusBadGateway, response.StatusCode, string(output))
					assert.Contains(t, string(output), `"error"`)
					assert.NotContains(t, string(output), "speech.audio.delta")
				} else {
					assert.Equal(t, http.StatusOK, response.StatusCode, string(output))
					if stream {
						assert.Contains(t, string(output), "event: speech.audio.delta")
						assert.Equal(t, scenario == "completed", strings.Contains(string(output), "event: speech.audio.done"))
						assert.Equal(t, scenario == "interrupted", strings.Contains(string(output), `"error"`))
					} else {
						assert.Equal(t, []byte{0, 1, 2, 3}, output)
					}
				}
				assertGatewayVolcSpeechAccounting(t, charge, 2000, scenario != "completed")
			})
		}
	}
}

func TestGatewayVolcSpeechQuotaDeniedBeforeWebSocket(t *testing.T) {
	var sends atomic.Int32
	url, key := setupGatewayVolcSpeech(t, func(w http.ResponseWriter, request *http.Request) {
		sends.Add(1)
		http.Error(w, "unexpected send", http.StatusInternalServerError)
	})
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Update("remain_quota", 100).Error)
	code, output := postGatewayTask(t, url, key, "/v1/audio/speech", `{"model":"tts-volc","input":"hello","voice":"alloy","response_format":"pcm"}`)
	assert.Equal(t, http.StatusForbidden, code, output)
	assert.Zero(t, sends.Load())
	assertGatewayVolcSpeechAccounting(t, 0, 100, false)
}

func TestGatewayVolcSpeechCancelledBeforeOutput(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%t", stream), func(t *testing.T) {
			var sends atomic.Int32
			requestRead := make(chan struct{})
			socketClosed := make(chan error, 1)
			url, key := setupGatewayVolcSpeech(t, func(w http.ResponseWriter, request *http.Request) {
				sends.Add(1)
				conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
				if err != nil {
					socketClosed <- err
					return
				}
				defer conn.Close()
				if _, err := volcengine.ReceiveMessage(conn); err != nil {
					socketClosed <- err
					return
				}
				close(requestRead)
				_, _, err = conn.ReadMessage()
				socketClosed <- err
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			streamField := ""
			if stream {
				streamField = `,"stream_format":"sse"`
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/audio/speech", strings.NewReader(`{"model":"tts-volc","input":"hello","voice":"alloy","response_format":"pcm"`+streamField+`}`))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+key)
			clientDone := make(chan error, 1)
			go func() {
				resp, err := http.DefaultClient.Do(request)
				if resp != nil {
					_ = resp.Body.Close()
				}
				clientDone <- err
			}()
			select {
			case <-requestRead:
			case <-time.After(3 * time.Second):
				t.Fatal("speech WebSocket request was not sent")
			}
			cancel()
			require.ErrorIs(t, <-clientDone, context.Canceled)
			select {
			case err := <-socketClosed:
				require.Error(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("client cancellation did not close the upstream WebSocket")
			}
			assert.EqualValues(t, 1, sends.Load())
			assertGatewayVolcSpeechAccounting(t, 0, 2000, false)
		})
	}
}
