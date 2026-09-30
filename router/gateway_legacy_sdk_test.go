package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayOfficialLegacySDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	// Response shapes match the existing legacy adaptor and buffered PaLM fixtures.
	providers := []struct {
		name, upstreamModel, key, jsonBody, prefix, terminal string
		channelType                                          int
	}{
		{"baidu", "ERNIE-Bot", "isolated-baidu-client|isolated-baidu-secret",
			`{"id":"baidu_1","result":"hello","usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			"data: {\"result\":\"hel\",\"is_end\":false}\n\ndata: {\"result\":\"lo\",\"is_end\":false}\n\n", "data: {\"result\":\"\",\"is_end\":true,\"usage\":{\"prompt_tokens\":10,\"total_tokens\":12}}\n\n", constant.ChannelTypeBaidu},
		{"cohere", "command", "isolated-cohere-key",
			`{"response_id":"cohere_1","text":"hello","finish_reason":"COMPLETE","meta":{"billed_units":{"input_tokens":10,"output_tokens":2}}}`,
			"{\"event_type\":\"text-generation\",\"text\":\"hel\",\"is_finished\":false}\n{\"event_type\":\"text-generation\",\"text\":\"lo\",\"is_finished\":false}\n", "{\"event_type\":\"stream-end\",\"is_finished\":true,\"finish_reason\":\"COMPLETE\",\"response\":{\"meta\":{\"billed_units\":{\"input_tokens\":10,\"output_tokens\":2}}}}\n", constant.ChannelTypeCohere},
		{"tencent", "hunyuan-lite", "1|isolated-tencent-id|isolated-tencent-secret",
			`{"Response":{"Id":"tencent_1","Choices":[{"Message":{"Content":"hello","Role":"assistant"},"FinishReason":"stop"}],"Usage":{"PromptTokens":10,"CompletionTokens":2,"TotalTokens":12}}}`,
			"data: {\"Choices\":[{\"Delta\":{\"Content\":\"hel\"}}]}\n\ndata: {\"Choices\":[{\"Delta\":{\"Content\":\"lo\"}}]}\n\n", "data: {\"Choices\":[{\"Delta\":{\"Content\":\"\"},\"FinishReason\":\"stop\"}],\"Usage\":{\"PromptTokens\":10,\"CompletionTokens\":2,\"TotalTokens\":12}}\n\n", constant.ChannelTypeTencent},
		{"zhipu", "chatglm_std", "isolated-zhipu-id.isolated-zhipu-secret",
			`{"success":true,"data":{"request_id":"zhipu_1","choices":[{"role":"assistant","content":"hello"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}`,
			"event: add\ndata: hel\n\nevent: add\ndata: lo\n\n", "event: finish\nmeta: {\"request_id\":\"zhipu_1\",\"task_status\":\"SUCCESS\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n", constant.ChannelTypeZhipu},
		{"dify", "gpt-4o-mini", "isolated-dify-key",
			`{"conversation_id":"dify_1","answer":"hello","metadata":{"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}`,
			"data: {\"event\":\"message\",\"answer\":\"hel\"}\n\ndata: {\"event\":\"message\",\"answer\":\"lo\"}\n\n", "data: {\"event\":\"message_end\",\"metadata\":{\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}}\n\n", constant.ChannelTypeDify},
		{"palm", "PaLM-2", "isolated-palm-key", `{"candidates":[{"content":"hello"}]}`, `{"candidates":[{"content":"hello"`, "", constant.ChannelTypePaLM},
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_legacy.py"}, {node, "verify_legacy.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			gatewayURL, key, _ := setupGatewaySDK(t)
			client := service.GetHttpClient()
			originalTransport := client.Transport
			t.Cleanup(func() { client.Transport = originalTransport })
			var lock sync.Mutex
			counts := make(map[string]int)
			client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == "aip.baidubce.com" && request.URL.Path == "/oauth/2.0/token" {
					assert.Equal(t, http.MethodPost, request.Method)
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"isolated-baidu-access","expires_in":7200}`)), Request: request}, nil
				}
				for _, provider := range providers {
					host := "legacy-" + provider.name + ".invalid"
					if provider.name == "tencent" {
						host = "hunyuan.tencentcloudapi.com"
					}
					if request.URL.Host != host {
						continue
					}
					body, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					label, mode := "", ""
					for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
						for _, responseMode := range []string{"json", "stream", "interrupt"} {
							candidate := "legacy-" + provider.name + "-" + protocol + "-" + responseMode
							if bytes.Contains(body, []byte(candidate)) {
								label, mode = candidate, responseMode
							}
						}
					}
					lock.Lock()
					counts[label]++
					lock.Unlock()
					valid := assert.NotEmpty(t, label)
					valid = assert.Contains(t, label, "-chat-") && valid
					valid = assert.Equal(t, http.MethodPost, request.Method) && valid
					stream := mode != "json"
					switch provider.name {
					case "baidu":
						valid = assert.Equal(t, "/rpc/2.0/ai_custom/v1/wenxinworkshop/chat/completions", request.URL.Path) && valid
						valid = assert.Equal(t, "isolated-baidu-access", request.URL.Query().Get("access_token")) && valid
						valid = assert.Equal(t, stream, gjson.GetBytes(body, "stream").Bool()) && valid
					case "cohere":
						valid = assert.Equal(t, "/v1/chat", request.URL.Path) && valid
						valid = assert.Equal(t, provider.upstreamModel, gjson.GetBytes(body, "model").String()) && valid
						valid = assert.Equal(t, "Bearer "+provider.key, request.Header.Get("Authorization")) && valid
						valid = assert.Equal(t, stream, gjson.GetBytes(body, "stream").Bool()) && valid
					case "tencent":
						valid = assert.Equal(t, "/", request.URL.Path) && valid
						valid = assert.Equal(t, provider.upstreamModel, gjson.GetBytes(body, "Model").String()) && valid
						valid = assert.Equal(t, stream, gjson.GetBytes(body, "Stream").Bool()) && valid
						valid = assert.True(t, strings.HasPrefix(request.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=isolated-tencent-id/")) && valid
						valid = assert.Equal(t, "ChatCompletions", request.Header.Get("X-TC-Action")) && valid
					case "zhipu":
						operation := "invoke"
						if stream {
							operation = "sse-invoke"
						}
						valid = assert.Equal(t, "/api/paas/v3/model-api/"+provider.upstreamModel+"/"+operation, request.URL.Path) && valid
						valid = assert.Len(t, strings.Split(request.Header.Get("Authorization"), "."), 3) && valid
					case "dify":
						valid = assert.Equal(t, "/v1/chat-messages", request.URL.Path, "this text fixture must never upload a file") && valid
						valid = assert.Equal(t, "Bearer "+provider.key, request.Header.Get("Authorization")) && valid
						expectedMode := "blocking"
						if stream {
							expectedMode = "streaming"
						}
						valid = assert.Equal(t, expectedMode, gjson.GetBytes(body, "response_mode").String()) && valid
					case "palm":
						valid = assert.Equal(t, "/v1beta2/models/chat-bison-001:generateMessage", request.URL.Path) && valid
						valid = assert.Equal(t, provider.key, request.Header.Get("X-Goog-Api-Key")) && valid
						valid = assert.False(t, gjson.GetBytes(body, "stream").Exists(), "PaLM buffers a non-streaming upstream response") && valid
					}
					if !valid {
						return nil, fmt.Errorf("invalid legacy fixture request")
					}
					recorder := httptest.NewRecorder()
					recorder.Header().Set("Content-Type", "application/json")
					response := provider.jsonBody
					if stream && provider.name != "palm" {
						recorder.Header().Set("Content-Type", "text/event-stream")
						if provider.name == "cohere" {
							recorder.Header().Set("Content-Type", "application/x-ndjson")
						}
						response = provider.prefix
						if mode != "interrupt" {
							response += provider.terminal
						}
					} else if mode == "interrupt" {
						response = provider.prefix
					}
					_, _ = io.WriteString(recorder, response)
					result := recorder.Result()
					result.Request = request
					return result, nil
				}
				return nil, fmt.Errorf("unexpected destination blocked by legacy SDK fixture")
			})
			prices := map[string]float64{}
			for _, provider := range providers {
				alias := "legacy-" + provider.name
				prices[alias] = 0.001
				mapping, err := common.Marshal(map[string]string{alias: provider.upstreamModel})
				require.NoError(t, err)
				mappingJSON, endpoint := string(mapping), "https://legacy-"+provider.name+".invalid"
				if provider.name == "tencent" {
					endpoint = "https://hunyuan.tencentcloudapi.com"
				}
				configured := &model.Channel{Type: provider.channelType, Name: "SDK legacy fixture", Models: alias, Key: provider.key, Status: common.ChannelStatusEnabled, BaseURL: &endpoint, Group: "default", CapacityTotal: 10, ChannelRatio: "1", ModelMapping: &mappingJSON}
				require.NoError(t, model.DB.Create(configured).Error)
				require.NoError(t, configured.AddAbilities(model.DB))
			}
			priceJSON, err := common.Marshal(prices)
			require.NoError(t, err)
			require.NoError(t, configureGatewayFixturePrices(string(priceJSON), false))
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
			for _, provider := range providers {
				for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
					for _, mode := range []string{"json", "stream", "interrupt"} {
						if mode == "interrupt" && protocol != "chat" {
							continue
						}
						label := "legacy-" + provider.name + "-" + protocol + "-" + mode
						expected := 0
						if protocol == "chat" {
							expected = 1
						}
						assert.Equal(t, expected, counts[label], label)
					}
				}
			}
			assert.Zero(t, counts[""])
		})
	}
}
