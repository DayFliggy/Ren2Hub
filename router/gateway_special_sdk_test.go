package router

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayOfficialSpecialSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_special.py"}, {node, "verify_special.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			gatewayURL, key, _ := setupGatewaySDK(t)
			var lock sync.Mutex
			counts := make(map[string]int)
			count := func(label string) int {
				lock.Lock()
				defer lock.Unlock()
				counts[label]++
				return counts[label]
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/spark" {
					assert.Equal(t, http.MethodGet, request.Method)
					assert.Equal(t, "kept", request.URL.Query().Get("fixture"))
					auth, err := base64.StdEncoding.DecodeString(request.URL.Query().Get("authorization"))
					assert.NoError(t, err)
					assert.Contains(t, string(auth), `username="isolated-xunfei-key"`)
					assert.NotEmpty(t, request.URL.Query().Get("date"))
					assert.Equal(t, request.Host, request.URL.Query().Get("host"))
					conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
					if !assert.NoError(t, err) {
						return
					}
					defer conn.Close()
					_, payload, err := conn.ReadMessage()
					if !assert.NoError(t, err) {
						return
					}
					label := gjson.GetBytes(payload, "payload.message.text.0.content").String()
					count(label)
					assert.True(t, strings.HasPrefix(label, "special-xunfei-chat-"), label)
					assert.Equal(t, "isolated-xunfei-app", gjson.GetBytes(payload, "header.app_id").String())
					assert.Equal(t, "generalv3.5", gjson.GetBytes(payload, "parameter.chat.domain").String())
					for _, text := range []string{"hel", "lo"} {
						frame := fmt.Sprintf(`{"header":{"code":0},"payload":{"choices":{"status":1,"text":[{"content":%q,"role":"assistant","index":0}]},"usage":{"text":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}}`, text)
						assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(frame)))
					}
					if !strings.HasSuffix(label, "-interrupt") {
						assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"header":{"code":0},"payload":{"choices":{"status":2,"text":[]},"usage":{"text":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}}`)))
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(request.URL.Path, "/model/") {
					payload, err := io.ReadAll(request.Body)
					assert.NoError(t, err)
					label := gjson.GetBytes(payload, "messages.0.content.0.text").String()
					count(label)
					assert.Equal(t, "special-nova-chat-json", label)
					assert.Equal(t, "/model/us.amazon.nova-lite-v1:0/invoke", request.URL.Path)
					assert.Equal(t, http.MethodPost, request.Method)
					assert.True(t, strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=isolated-nova-access/"))
					assert.Equal(t, "messages-v1", gjson.GetBytes(payload, "schemaVersion").String())
					_, _ = io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"hel"},{"text":"lo"}]}},"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":2,"totalTokens":12}}`)
					return
				}
				assert.Equal(t, "Bearer isolated-coze-key", request.Header.Get("Authorization"))
				switch request.URL.Path {
				case "/v3/chat":
					assert.Equal(t, http.MethodPost, request.Method)
					payload, err := io.ReadAll(request.Body)
					assert.NoError(t, err)
					label := gjson.GetBytes(payload, "additional_messages.0.content").String()
					count(label)
					assert.True(t, strings.HasPrefix(label, "special-coze-chat-"), label)
					assert.Equal(t, "isolated-bot", gjson.GetBytes(payload, "bot_id").String())
					assert.NotEmpty(t, gjson.GetBytes(payload, "user_id").String())
					if !gjson.GetBytes(payload, "stream").Bool() {
						_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"conversation_id":"conversation?fixture"}}`, label)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					for _, text := range []string{"hel", "lo"} {
						_, _ = fmt.Fprintf(w, "event: conversation.message.delta\ndata: {\"type\":\"answer\",\"content_type\":\"text\",\"content\":%q}\n\n", text)
					}
					if !strings.HasSuffix(label, "-interrupt") {
						_, _ = io.WriteString(w, "event: conversation.chat.completed\ndata: {\"usage\":{\"input_count\":10,\"output_count\":2,\"token_count\":12}}\n\n")
					}
				case "/v3/chat/retrieve", "/v3/chat/message/list":
					assert.Equal(t, http.MethodGet, request.Method)
					assert.Equal(t, "conversation?fixture", request.URL.Query().Get("conversation_id"))
					label := request.URL.Query().Get("chat_id")
					assert.Contains(t, []string{"special-coze-chat-json", "special-coze-chat-poll_failure"}, label)
					visits := count(label + request.URL.Path)
					if strings.HasSuffix(label, "-poll_failure") {
						http.Error(w, `{"message":"poll temporarily unavailable"}`, http.StatusBadGateway)
						return
					}
					if request.URL.Path == "/v3/chat/message/list" {
						_, _ = io.WriteString(w, `{"data":[{"type":"answer","content_type":"text","content":"hel"},{"type":"answer","content_type":"text","content":"lo"}]}`)
						return
					}
					status := "completed"
					if visits == 1 {
						status = "in_progress"
					}
					_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"conversation_id":"conversation?fixture","status":%q,"usage":{"input_count":10,"output_count":2,"token_count":12}}}`, label, status)
				default:
					t.Errorf("unexpected local provider path: %s", request.URL.Path)
					http.Error(w, "unexpected local provider path", http.StatusNotFound)
				}
			}))
			t.Cleanup(provider.Close)
			localURL, err := url.Parse(provider.URL)
			require.NoError(t, err)
			client := service.GetHttpClient()
			originalTransport := client.Transport
			localTransport := http.DefaultTransport.(*http.Transport).Clone()
			localTransport.Proxy = nil
			t.Cleanup(func() {
				client.Transport = originalTransport
				localTransport.CloseIdleConnections()
			})
			client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != localURL.Host && request.URL.Host != "bedrock-runtime.us-east-1.amazonaws.com" {
					return nil, fmt.Errorf("unexpected destination blocked by special SDK fixture")
				}
				localRequest := request.Clone(request.Context())
				localRequest.URL.Scheme, localRequest.URL.Host = localURL.Scheme, localURL.Host
				return localTransport.RoundTrip(localRequest)
			})
			prices := map[string]float64{}
			for _, config := range []struct {
				alias, upstreamModel, key, endpoint string
				channelType                         int
			}{
				{"special-nova", "nova-lite-v1:0", "isolated-nova-access|isolated-nova-secret|us-east-1", "https://bedrock-runtime.us-east-1.amazonaws.com", constant.ChannelTypeAws},
				{"special-coze", "coze-sdk", "isolated-coze-key", provider.URL, constant.ChannelTypeCoze},
				{"special-coze-poll-failure", "coze-sdk", "isolated-coze-key", provider.URL, constant.ChannelTypeCoze},
				{"special-xunfei", "SparkDesk-v3.5", "isolated-xunfei-app|isolated-xunfei-secret|isolated-xunfei-key", provider.URL + "/spark?fixture=kept", constant.ChannelTypeXunfei},
			} {
				prices[config.alias] = 0.001
				mapping, err := common.Marshal(map[string]string{config.alias: config.upstreamModel})
				require.NoError(t, err)
				mappingJSON, other := string(mapping), "isolated-bot"
				configured := &model.Channel{Type: config.channelType, Name: "SDK special fixture", Models: config.alias, Key: config.key, Status: common.ChannelStatusEnabled, BaseURL: &config.endpoint,
					Group: "default", CapacityTotal: 10, ChannelRatio: 1, ModelMapping: &mappingJSON, Other: other}
				require.NoError(t, model.DB.Create(configured).Error)
				require.NoError(t, configured.AddAbilities(model.DB))
			}
			priceJSON, err := common.Marshal(prices)
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(priceJSON)))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+gatewayURL, "GATEWAY_SDK_KEY="+key)
			output, err := command.CombinedOutput()
			assert.NoError(t, err, "%s", output)
			t.Log(string(output))
			lock.Lock()
			defer lock.Unlock()
			for _, provider := range []string{"nova", "coze", "xunfei"} {
				for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
					for _, mode := range []string{"json", "stream"} {
						label := "special-" + provider + "-" + protocol + "-" + mode
						expected := 0
						if protocol == "chat" && (provider != "nova" || mode == "json") {
							expected = 1
						}
						assert.Equal(t, expected, counts[label], label)
					}
				}
			}
			assert.Equal(t, 1, counts["special-coze-chat-interrupt"])
			assert.Equal(t, 1, counts["special-xunfei-chat-interrupt"])
			assert.Equal(t, 1, counts["special-coze-chat-poll_failure"], "an accepted chat must never be created twice after a failed status query")
			assert.Equal(t, 2, counts["special-coze-chat-json/v3/chat/retrieve"])
			assert.Equal(t, 1, counts["special-coze-chat-json/v3/chat/message/list"])
			assert.Equal(t, 1, counts["special-coze-chat-poll_failure/v3/chat/retrieve"])
			assert.Zero(t, counts["special-coze-chat-poll_failure/v3/chat/message/list"])
			assert.Zero(t, counts[""])
		})
	}
}
