package router

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayOfficialAudioSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_audio.py"}, {node, "verify_audio.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			var mini, volcHTTP, volcWS, cloud atomic.Int32
			completeFrame, partialFrame := gatewayVolcAudioFrame(t, -1), gatewayVolcAudioFrame(t, 1)
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				if websocket.IsWebSocketUpgrade(r) {
					volcWS.Add(1)
					conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
					require.NoError(t, err)
					defer conn.Close()
					message, err := volcengine.ReceiveMessage(conn)
					require.NoError(t, err)
					var request volcengine.VolcengineTTSRequest
					require.NoError(t, common.Unmarshal(message.Payload, &request))
					assert.Equal(t, "speech-volc-ws", request.Request.Model)
					frame := completeFrame
					if request.Request.Text == "interrupt" {
						frame = partialFrame
					}
					require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, frame))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/ai/run/") {
					cloud.Add(1)
					audio, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					assert.Equal(t, "RIFF", string(audio[:min(4, len(audio))]))
					_, _ = io.WriteString(w, `{"success":true,"result":{"text":"hello"}}`)
					return
				}
				var request map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &request))
				if nested, ok := request["request"].(map[string]any); ok {
					volcHTTP.Add(1)
					assert.Equal(t, "hello", nested["text"])
					_, _ = io.WriteString(w, `{"code":3000,"data":"AAECAw=="}`)
					return
				}
				mini.Add(1)
				assert.Equal(t, "speech-minimax", request["model"])
				assert.Equal(t, "hello", request["text"])
				_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"audio":"00010203","status":2}}`)
			})
			require.NoError(t, publishGatewayFixturePrices(`{"speech-minimax":0.001,"speech-volc-http":0.001,"speech-volc-ws":0.001,"whisper-cloudflare":0.001}`, false))
			for _, config := range []struct {
				id, channelType int
				name, key       string
			}{
				{1, constant.ChannelTypeMiniMax, "speech-minimax", "local-mini"},
				{2, constant.ChannelTypeVolcEngine, "speech-volc-http", "local-app|local-token"},
				{3, constant.ChannelTypeVolcEngine, "speech-volc-ws", "local-app|local-token"},
				{4, constant.ChannelCloudflare, "whisper-cloudflare", "local-cloud"},
			} {
				channel, err := model.GetChannelById(config.id, true)
				require.NoError(t, err)
				channel.Type, channel.Models, channel.Key = config.channelType, config.name, config.key
				base := *channel.BaseURL
				if config.id == 3 {
					base = "ws" + strings.TrimPrefix(base, "http")
				}
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "key": channel.Key, "base_url": base}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
			}
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 10000}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+url, "GATEWAY_SDK_KEY="+key)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Log(string(output))
			assert.EqualValues(t, 1, mini.Load())
			assert.EqualValues(t, 1, volcHTTP.Load())
			assert.EqualValues(t, 2, volcWS.Load())
			assert.EqualValues(t, 2, cloud.Load(), "unsupported requests must not reach the provider")
			awaitGatewayPermitsReleased(t)
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			assert.Len(t, logs, 6, "partial speech settles once; unsupported paths do not charge")
		})
	}
}
