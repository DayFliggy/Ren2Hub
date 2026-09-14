package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
)

func TestGatewayChannelSystemPromptAcrossProtocols(t *testing.T) {
	for _, upstreamModel := range []string{"gpt-4o-mini", "claude-3-5-sonnet", "gemini-2.5-flash"} {
		for _, protocol := range []string{"chat", "responses", "claude", "gemini"} {
			for _, passthrough := range []bool{false, true} {
				native := (upstreamModel == "gpt-4o-mini" && (protocol == "chat" || protocol == "responses")) ||
					(upstreamModel == "claude-3-5-sonnet" && protocol == "claude") || (upstreamModel == "gemini-2.5-flash" && protocol == "gemini")
				if passthrough && !native {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/passthrough=%t", upstreamModel, protocol, passthrough), func(t *testing.T) {
					var mu sync.Mutex
					var observed []map[string]any
					var paths []string
					url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if !assert.NoError(t, err) {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						var value map[string]any
						if !assert.NoError(t, common.Unmarshal(body, &value)) {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						mu.Lock()
						observed, paths = append(observed, value), append(paths, r.URL.Path)
						mu.Unlock()
						r.Body = io.NopCloser(strings.NewReader(string(body)))
						gatewaySDKUpstream(w, r)
					})
					setting, err := common.Marshal(map[string]any{"system_prompt": "channel-rule", "system_prompt_override": true, "pass_through_body_enabled": passthrough})
					require.NoError(t, err)
					mapping, err := common.Marshal(map[string]string{upstreamModel: upstreamModel + "-mapped"})
					require.NoError(t, err)
					require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", upstreamModel).Updates(map[string]any{"setting": string(setting), "model_mapping": string(mapping)}).Error)
					require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
					path := "/v1/chat/completions"
					body := map[string]any{"model": upstreamModel, "messages": []any{map[string]any{"role": "system", "content": "client-rule"}, map[string]any{"role": "user", "content": "hello"}}}
					switch protocol {
					case "responses":
						path, body = "/v1/responses", map[string]any{"model": upstreamModel, "instructions": "client-rule", "input": "hello"}
					case "claude":
						path, body = "/v1/messages", map[string]any{"model": upstreamModel, "max_tokens": 100, "system": "client-rule", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
					case "gemini":
						path, body = "/v1beta/models/"+upstreamModel+":generateContent", map[string]any{"system_instruction": map[string]any{"parts": []any{map[string]any{"text": "client-rule"}}}, "contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}}}
					}
					if passthrough {
						require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("conversion_policy", "strict").Error)
						body["vendor_extension"] = map[string]any{"false": false, "zero": 0}
						if protocol == "chat" {
							body["messages"].([]any)[0].(map[string]any)["vendor_message"] = false
						}
					}
					encoded, err := common.Marshal(body)
					require.NoError(t, err)
					request, err := http.NewRequest(http.MethodPost, url+path, strings.NewReader(string(encoded)))
					require.NoError(t, err)
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Authorization", "Bearer "+key)
					response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
					require.NoError(t, err)
					defer response.Body.Close()
					result, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, response.StatusCode, string(result))
					mu.Lock()
					defer mu.Unlock()
					require.Len(t, observed, 1)
					actual := observed[0]
					if upstreamModel == "gemini-2.5-flash" {
						assert.Contains(t, paths[0], upstreamModel+"-mapped:")
					} else {
						assert.Equal(t, upstreamModel+"-mapped", actual["model"])
					}
					var system any
					switch {
					case strings.Contains(paths[0], "generateContent"):
						system = actual["systemInstruction"]
						assert.NotContains(t, actual, "system_instruction")
					case strings.Contains(paths[0], "responses"):
						system = actual["instructions"]
					case strings.Contains(paths[0], "messages"):
						system = actual["system"]
					default:
						system = actual["messages"].([]any)[0]
					}
					encoded, err = common.Marshal(system)
					require.NoError(t, err)
					assert.Equal(t, 1, strings.Count(string(encoded), "channel-rule"), string(encoded))
					assert.Contains(t, string(encoded), "client-rule")
					if passthrough {
						assert.Equal(t, map[string]any{"false": false, "zero": float64(0)}, actual["vendor_extension"])
						if protocol == "chat" {
							assert.Equal(t, false, actual["messages"].([]any)[0].(map[string]any)["vendor_message"])
						}
					}
				})
			}
		}
	}
}

func TestGatewayNovaReceivesChannelSystemPrompt(t *testing.T) {
	url, key, _ := setupGatewaySDK(t)
	var selected model.Channel
	require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeOpenAI).Order("id").First(&selected).Error)
	require.NoError(t, model.DB.Model(&selected).Updates(map[string]any{
		"type": constant.ChannelTypeAws, "key": "isolated-test-token|us-east-1",
		"model_mapping": `{"gpt-4o-mini":"nova-lite"}`, "setting": `{"system_prompt":"channel-rule"}`,
	}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	client := service.GetHttpClient()
	originalTransport := client.Transport
	t.Cleanup(func() { client.Transport = originalTransport })
	var sends int
	client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
		sends++
		var actual map[string]any
		if !assert.NoError(t, common.DecodeJson(request.Body, &actual)) {
			return nil, fmt.Errorf("invalid AWS request JSON")
		}
		assert.Equal(t, []any{map[string]any{"text": "channel-rule"}}, actual["system"])
		assert.Equal(t, []any{map[string]any{"role": "user", "content": []any{map[string]any{"text": "hello"}}}}, actual["messages"])
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request,
			Body: io.NopCloser(strings.NewReader(`{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":12,"outputTokens":2,"totalTokens":14}}`))}, nil
	})
	request, err := http.NewRequest(http.MethodPost, url+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Contains(t, string(body), "hello")
	assert.Equal(t, 1, sends)
}

type gatewayPromptTransport func(*http.Request) (*http.Response, error)

func TestGatewayAWSPassthroughUsesPreparedBody(t *testing.T) {
	url, key, _ := setupGatewaySDK(t)
	var selected model.Channel
	require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeAnthropic).First(&selected).Error)
	require.NoError(t, model.DB.Model(&selected).Updates(map[string]any{
		"type": constant.ChannelTypeAws, "key": "isolated-test-token|us-east-1",
		"setting":        `{"system_prompt":"channel-rule","system_prompt_override":true,"pass_through_body_enabled":true}`,
		"param_override": `{"max_tokens":64}`,
	}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	client := service.GetHttpClient()
	original := client.Transport
	t.Cleanup(func() { client.Transport = original })
	var sends int
	client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
		sends++
		var actual map[string]any
		if !assert.NoError(t, common.DecodeJson(request.Body, &actual)) {
			return nil, fmt.Errorf("invalid AWS JSON")
		}
		assert.Equal(t, "channel-rule\nclient-rule", actual["system"])
		assert.Equal(t, float64(64), actual["max_tokens"])
		assert.Equal(t, false, actual["extension"])
		assert.Equal(t, "bedrock-2023-05-31", actual["anthropic_version"])
		assert.NotContains(t, actual, "model")
		assert.NotContains(t, actual, "stream")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request,
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":2}}`))}, nil
	})
	request, err := http.NewRequest(http.MethodPost, url+"/v1/messages", strings.NewReader(`{"model":"claude-3-5-sonnet","system":"client-rule","messages":[{"role":"user","content":"hello"}],"max_tokens":10,"extension":false}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, 200, response.StatusCode, string(body))
	assert.Equal(t, 1, sends)
}

func (f gatewayPromptTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
