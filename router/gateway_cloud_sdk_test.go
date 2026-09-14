package router

import (
	"bufio"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayOfficialCloudSDK(t *testing.T) {
	runGatewayCloudSDK(t, "text")
}

func TestGatewayOfficialCloudToolAndInterruptionSDK(t *testing.T) {
	runGatewayCloudSDK(t, "tools_and_interrupts")
}

func TestGatewayOfficialCloudParallelToolSDK(t *testing.T) {
	runGatewayCloudSDK(t, "parallel_tools")
}

func runGatewayCloudSDK(t *testing.T, phase string) {
	t.Helper()
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	for _, provider := range []struct {
		name, upstreamModel, key, settings string
		channelType                        int
	}{
		{"azure", "cloud-deployment", "isolated-azure-key", "{}", constant.ChannelTypeAzure},
		{"aws_claude_bearer", "claude-3-5-sonnet-20240620", "isolated-bedrock-token|us-east-1", "{}", constant.ChannelTypeAws},
		{"aws_claude_sigv4", "claude-3-5-sonnet-20240620", "isolated-access|isolated-secret|us-east-1", "{}", constant.ChannelTypeAws},
		{"vertex_claude", "claude-3-5-sonnet-20240620", "isolated-vertex-key", `{"vertex_key_type":"api_key"}`, constant.ChannelTypeVertexAi},
		{"vertex_gemini", "gemini-2.5-flash", "isolated-vertex-key", `{"vertex_key_type":"api_key"}`, constant.ChannelTypeVertexAi},
	} {
		for _, runtime := range []struct{ binary, script string }{{python, "verify_cloud.py"}, {node, "verify_cloud.mjs"}} {
			t.Run(provider.name+"/"+runtime.script, func(t *testing.T) {
				gatewayURL, key, _ := setupGatewaySDK(t)
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id <> ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
				mapping, err := common.Marshal(map[string]string{"gpt-4o-mini": provider.upstreamModel})
				require.NoError(t, err)
				version := "global"
				if provider.channelType == constant.ChannelTypeAzure {
					version = "2024-06-01"
				}
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{
					"type": provider.channelType, "key": provider.key, "base_url": "https://cloud-sdk.invalid",
					"model_mapping": string(mapping), "settings": provider.settings, "other": version,
				}).Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				client := service.GetHttpClient()
				originalTransport := client.Transport
				t.Cleanup(func() { client.Transport = originalTransport })
				var lock sync.Mutex
				counts := make(map[string]int)
				client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
					// Every gateway outbound request is intercepted, including AWS SDK
					// requests after signing. Unknown destinations never reach a network.
					if request.URL.Host != "cloud-sdk.invalid" && request.URL.Host != "bedrock-runtime.us-east-1.amazonaws.com" {
						return nil, fmt.Errorf("unexpected cloud fixture destination")
					}
					body, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
						for _, mode := range []string{"json", "stream"} {
							label := "cloud-" + protocol + "-" + mode
							if bytes.Contains(body, []byte(label)) {
								lock.Lock()
								counts[label]++
								lock.Unlock()
							}
						}
					}
					toolRequest := bytes.Contains(body, []byte("use_tool"))
					parallelRequest := bytes.Contains(body, []byte("use_parallel_tools"))
					toolResult := bytes.Contains(body, []byte("function_call_output")) || bytes.Contains(body, []byte(`"role":"tool"`)) ||
						bytes.Contains(body, []byte("tool_result")) || bytes.Contains(body, []byte("functionResponse"))
					interrupted := bytes.Contains(body, []byte("interrupt"))
					if phase == "parallel_tools" && parallelRequest {
						category := "parallel_stream"
						if toolResult {
							category = "parallel_result"
						}
						lock.Lock()
						counts[category]++
						lock.Unlock()
					}
					if phase == "tools_and_interrupts" {
						category := "health_recovery"
						switch {
						case toolRequest && toolResult:
							category = "tool_result"
						case toolRequest:
							category = "tool_stream"
						case interrupted:
							category = "interruption"
						}
						lock.Lock()
						counts[category]++
						lock.Unlock()
					}
					stream := gjson.GetBytes(body, "stream").Bool() || strings.Contains(request.URL.Path, "streamRawPredict") ||
						strings.Contains(request.URL.Path, "streamGenerateContent") || strings.HasSuffix(request.URL.Path, "invoke-with-response-stream")
					responsePath := "/v1/chat/completions"
					valid := assert.Equal(t, http.MethodPost, request.Method)
					expectStream := bytes.Contains(body, []byte("-stream")) || (toolRequest || parallelRequest) && !toolResult || interrupted
					valid = assert.Equal(t, expectStream, stream, "the upstream operation must match the requested response mode") && valid
					switch provider.channelType {
					case constant.ChannelTypeAzure:
						valid = assert.Equal(t, provider.key, request.Header.Get("api-key")) && valid
						valid = assert.NotEmpty(t, request.URL.Query().Get("api-version")) && valid
						valid = assert.Equal(t, provider.upstreamModel, gjson.GetBytes(body, "model").String()) && valid
						if strings.Contains(request.URL.Path, "responses") {
							responsePath = "/v1/responses"
							valid = assert.Equal(t, "/openai/v1/responses", request.URL.Path) && valid
							valid = assert.True(t, gjson.GetBytes(body, "input").Exists()) && valid
							valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "max_output_tokens").Int()) && valid
						} else {
							valid = assert.Equal(t, "/openai/deployments/cloud-deployment/chat/completions", request.URL.Path) && valid
							valid = assert.NotEmpty(t, gjson.GetBytes(body, "messages").Array()) && valid
						}
					case constant.ChannelTypeAws:
						responsePath = "/v1/messages"
						valid = assert.Contains(t, request.URL.Path, "us.anthropic.claude-3-5-sonnet-20240620-v1:0") && valid
						action := "/invoke"
						if stream {
							action = "/invoke-with-response-stream"
						}
						valid = assert.True(t, strings.HasSuffix(request.URL.Path, action)) && valid
						valid = assert.Equal(t, "bedrock-2023-05-31", gjson.GetBytes(body, "anthropic_version").String()) && valid
						valid = assert.False(t, gjson.GetBytes(body, "model").Exists()) && valid
						valid = assert.False(t, gjson.GetBytes(body, "stream").Exists()) && valid
						if strings.HasSuffix(provider.name, "sigv4") {
							valid = assert.True(t, strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ")) && valid
							valid = assert.NotEmpty(t, request.Header.Get("X-Amz-Date")) && valid
						} else {
							valid = assert.Equal(t, "Bearer isolated-bedrock-token", request.Header.Get("Authorization")) && valid
						}
					case constant.ChannelTypeVertexAi:
						valid = assert.Equal(t, provider.key, request.URL.Query().Get("key")) && valid
						if provider.name == "vertex_claude" {
							responsePath = "/v1/messages"
							valid = assert.Contains(t, request.URL.Path, "/publishers/anthropic/models/claude-3-5-sonnet@20240620:") && valid
							action := ":rawPredict"
							if stream {
								action = ":streamRawPredict"
							}
							valid = assert.True(t, strings.HasSuffix(request.URL.Path, action)) && valid
							valid = assert.Equal(t, "vertex-2023-10-16", gjson.GetBytes(body, "anthropic_version").String()) && valid
						} else {
							responsePath = "/v1beta/models/gemini-2.5-flash:generateContent"
							if stream {
								responsePath = "/v1beta/models/gemini-2.5-flash:streamGenerateContent"
							}
							valid = assert.Contains(t, request.URL.Path, "/publishers/google/models/gemini-2.5-flash:") && valid
							valid = assert.True(t, strings.HasSuffix(request.URL.Path, strings.Split(responsePath, ":")[1])) && valid
							valid = assert.NotEmpty(t, gjson.GetBytes(body, "contents").Array(), "Vertex Gemini requires Gemini contents") && valid
							valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "generationConfig.maxOutputTokens").Int()) && valid
							valid = assert.Equal(t, "0", gjson.GetBytes(body, "generationConfig.temperature").Raw, "explicit zero temperature must survive conversion") && valid
						}
					}
					if responsePath == "/v1/messages" {
						valid = assert.NotEmpty(t, gjson.GetBytes(body, "messages").Array(), "Claude adapter must send messages") && valid
						valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "max_tokens").Int()) && valid
					}
					if provider.name != "vertex_gemini" {
						valid = assert.Equal(t, "0", gjson.GetBytes(body, "temperature").Raw, "explicit zero temperature must survive conversion") && valid
					}
					if toolRequest {
						valid = validateGatewayCloudToolRequest(t, body, responsePath, toolResult) && valid
					}
					if parallelRequest {
						valid = validateGatewayCloudParallelToolRequest(t, body, responsePath, toolResult) && valid
					}
					if !valid {
						return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request,
							Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"cloud SDK fixture rejected invalid native request"}}`))}, nil
					}
					var payload map[string]any
					if err := common.Unmarshal(body, &payload); err != nil {
						return nil, err
					}
					payload["stream"], payload["model"] = stream, provider.upstreamModel
					fixtureBody, err := common.Marshal(payload)
					if err != nil {
						return nil, err
					}
					recorder := httptest.NewRecorder()
					if parallelRequest && !toolResult {
						gatewayCloudParallelToolsResponse(recorder, responsePath, provider.upstreamModel)
					} else {
						gatewaySDKUpstream(recorder, httptest.NewRequest(http.MethodPost, responsePath, bytes.NewReader(fixtureBody)))
					}
					response := recorder.Result()
					response.Request = request
					if provider.name == "vertex_gemini" && (toolRequest || parallelRequest) && !toolResult {
						defer response.Body.Close()
						body, err := gatewayCloudVertexToolResponse(response.Body)
						if err != nil {
							return nil, err
						}
						response.Body = io.NopCloser(bytes.NewReader(body))
					}
					if provider.channelType != constant.ChannelTypeAws || !stream {
						return response, nil
					}
					defer response.Body.Close()
					var encoded bytes.Buffer
					scanner := bufio.NewScanner(response.Body)
					for scanner.Scan() {
						if !strings.HasPrefix(scanner.Text(), "data: ") {
							continue
						}
						payload, err := common.Marshal(struct {
							Bytes []byte `json:"bytes"`
						}{Bytes: []byte(strings.TrimPrefix(scanner.Text(), "data: "))})
						if err != nil {
							return nil, err
						}
						if err := eventstream.NewEncoder().Encode(&encoded, eventstream.Message{Headers: eventstream.Headers{
							{Name: eventstreamapi.MessageTypeHeader, Value: eventstream.StringValue(eventstreamapi.EventMessageType)},
							{Name: eventstreamapi.EventTypeHeader, Value: eventstream.StringValue("chunk")},
							{Name: eventstreamapi.ContentTypeHeader, Value: eventstream.StringValue("application/json")},
						}, Payload: payload}); err != nil {
							return nil, err
						}
					}
					if err := scanner.Err(); err != nil {
						return nil, err
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"},
						"X-Amzn-Bedrock-Content-Type": []string{"application/json"}}, Request: request, Body: io.NopCloser(bytes.NewReader(encoded.Bytes()))}, nil
				})
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
				command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+gatewayURL, "GATEWAY_SDK_KEY="+key, "GATEWAY_CLOUD_PROVIDER="+provider.name, "GATEWAY_CLOUD_PHASE="+phase)
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				t.Log(string(output))
				lock.Lock()
				defer lock.Unlock()
				if phase == "tools_and_interrupts" {
					for _, category := range []string{"tool_stream", "tool_result", "interruption", "health_recovery"} {
						assert.Equal(t, 4, counts[category], "each protocol uses one real adapter attempt for %s", category)
					}
					return
				}
				if phase == "parallel_tools" {
					assert.Equal(t, 4, counts["parallel_stream"])
					assert.Equal(t, 4, counts["parallel_result"], "every protocol must return both tool results in one adapter attempt")
					return
				}
				for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
					for _, mode := range []string{"json", "stream"} {
						assert.Equal(t, 1, counts["cloud-"+protocol+"-"+mode], "each SDK call must use the selected real adapter once")
					}
				}
			})
		}
	}
}

func gatewayCloudParallelToolsResponse(w http.ResponseWriter, path, modelName string) {
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(value map[string]any) {
		data, _ := common.Marshal(value)
		if event, ok := value["type"]; ok {
			fmt.Fprintf(w, "event: %s\n", event)
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	values := []any{0, false}
	names := []string{"echo_zero", "echo_false"}
	ids := []string{"call_zero", "call_false"}
	switch {
	case strings.Contains(path, "enerateContent"):
		parts := make([]any, 0, 2)
		for i, name := range names {
			parts = append(parts, map[string]any{"functionCall": map[string]any{"id": ids[i], "name": name, "args": map[string]any{"value": values[i]}}})
		}
		write(map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}},
			"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 4, "totalTokenCount": 14}})
	case path == "/v1/messages":
		usage := map[string]any{"input_tokens": 10, "output_tokens": 4}
		write(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_parallel", "type": "message", "role": "assistant",
			"model": modelName, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage}})
		for i, name := range names {
			write(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": ids[i], "name": name, "input": map[string]any{}}})
			write(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"value":`}})
			tail, _ := common.Marshal(values[i])
			write(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(tail) + "}"}})
			write(map[string]any{"type": "content_block_stop", "index": i})
		}
		write(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": usage})
		write(map[string]any{"type": "message_stop"})
	case path == "/v1/responses":
		sequence := 0
		event := func(value map[string]any) {
			value["sequence_number"] = sequence
			sequence++
			write(value)
		}
		event(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_parallel", "object": "response", "created_at": 1, "status": "in_progress", "model": modelName, "output": []any{}}})
		items := make([]any, 0, 2)
		for i, name := range names {
			event(map[string]any{"type": "response.output_item.added", "output_index": i, "item": map[string]any{"id": "fc_" + ids[i], "type": "function_call", "call_id": ids[i], "name": name, "arguments": "", "status": "in_progress"}})
		}
		for i, name := range names {
			itemID := "fc_" + ids[i]
			event(map[string]any{"type": "response.function_call_arguments.delta", "item_id": itemID, "output_index": i, "delta": `{"value":`})
			tail, _ := common.Marshal(values[i])
			event(map[string]any{"type": "response.function_call_arguments.delta", "item_id": itemID, "output_index": i, "delta": string(tail) + "}"})
			args, _ := common.Marshal(map[string]any{"value": values[i]})
			event(map[string]any{"type": "response.function_call_arguments.done", "item_id": itemID, "output_index": i, "arguments": string(args)})
			item := map[string]any{"id": itemID, "type": "function_call", "call_id": ids[i], "name": name, "arguments": string(args), "status": "completed"}
			items = append(items, item)
			event(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		}
		event(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_parallel", "object": "response", "created_at": 1,
			"status": "completed", "model": modelName, "output": items, "usage": map[string]any{"input_tokens": 10, "output_tokens": 4, "total_tokens": 14}}})
	default:
		first, last := make([]any, 0, 2), make([]any, 0, 2)
		for i, name := range names {
			first = append(first, map[string]any{"index": i, "id": ids[i], "type": "function", "function": map[string]any{"name": name, "arguments": `{"value":`}})
			tail, _ := common.Marshal(values[i])
			last = append(last, map[string]any{"index": i, "function": map[string]any{"arguments": string(tail) + "}"}})
		}
		for _, choice := range []map[string]any{
			{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": first}, "finish_reason": nil},
			{"index": 0, "delta": map[string]any{"tool_calls": last}, "finish_reason": "tool_calls"},
		} {
			write(map[string]any{"id": "chat_parallel", "object": "chat.completion.chunk", "created": 1, "model": modelName, "choices": []any{choice}})
		}
		write(map[string]any{"id": "chat_parallel", "object": "chat.completion.chunk", "created": 1, "model": modelName, "choices": []any{},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4, "total_tokens": 14}})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func validateGatewayCloudParallelToolRequest(t *testing.T, body []byte, path string, withResult bool) bool {
	t.Helper()
	type toolValue struct{ id, name, payload string }
	var declarations []string
	var calls, results []toolValue
	request := gjson.ParseBytes(body)
	switch {
	case strings.Contains(path, "enerateContent"):
		for _, tool := range request.Get("tools").Array() {
			for _, function := range tool.Get("functionDeclarations").Array() {
				declarations = append(declarations, function.Get("name").String())
			}
		}
		for _, content := range request.Get("contents").Array() {
			for _, part := range content.Get("parts").Array() {
				if call := part.Get("functionCall"); call.Exists() {
					calls = append(calls, toolValue{call.Get("id").String(), call.Get("name").String(), call.Get("args").Raw})
				}
				if result := part.Get("functionResponse"); result.Exists() {
					results = append(results, toolValue{result.Get("id").String(), result.Get("name").String(), result.Get("response").Raw})
				}
			}
		}
	case path == "/v1/responses":
		for _, tool := range request.Get("tools").Array() {
			declarations = append(declarations, tool.Get("name").String())
		}
		for _, item := range request.Get("input").Array() {
			if item.Get("type").String() == "function_call" {
				calls = append(calls, toolValue{item.Get("call_id").String(), item.Get("name").String(), item.Get("arguments").String()})
			}
			if item.Get("type").String() == "function_call_output" {
				results = append(results, toolValue{id: item.Get("call_id").String(), payload: item.Get("output").String()})
			}
		}
	case path == "/v1/messages":
		for _, tool := range request.Get("tools").Array() {
			declarations = append(declarations, tool.Get("name").String())
		}
		for _, message := range request.Get("messages").Array() {
			for _, block := range message.Get("content").Array() {
				if block.Get("type").String() == "tool_use" {
					calls = append(calls, toolValue{block.Get("id").String(), block.Get("name").String(), block.Get("input").Raw})
				}
				if block.Get("type").String() == "tool_result" {
					content := block.Get("content")
					if content.IsArray() {
						content = content.Get("0.text")
					}
					results = append(results, toolValue{id: block.Get("tool_use_id").String(), payload: content.String()})
				}
			}
		}
	default:
		for _, tool := range request.Get("tools").Array() {
			declarations = append(declarations, tool.Get("function.name").String())
		}
		for _, message := range request.Get("messages").Array() {
			for _, call := range message.Get("tool_calls").Array() {
				calls = append(calls, toolValue{call.Get("id").String(), call.Get("function.name").String(), call.Get("function.arguments").String()})
			}
			if message.Get("role").String() == "tool" {
				results = append(results, toolValue{id: message.Get("tool_call_id").String(), payload: message.Get("content").String()})
			}
		}
	}
	valid := assert.ElementsMatch(t, []string{"echo_zero", "echo_false"}, declarations)
	if !withResult {
		return valid
	}
	valid = assert.Len(t, calls, 2) && valid
	valid = assert.Len(t, results, 2) && valid
	namesByID := make(map[string]string)
	seen := make(map[string]bool)
	for _, call := range calls {
		expected := "0"
		id := "call_zero"
		if call.name == "echo_false" {
			expected, id = "false", "call_false"
		}
		valid = assert.Contains(t, declarations, call.name) && valid
		valid = assert.False(t, seen[call.name], "both distinct tool calls must survive") && valid
		seen[call.name] = true
		if strings.Contains(path, "enerateContent") {
			id = ""
		}
		valid = assert.Equal(t, id, call.id) && valid
		valid = assert.JSONEq(t, `{"value":`+expected+"}", call.payload) && valid
		namesByID[call.id] = call.name
	}
	seen = make(map[string]bool)
	for _, result := range results {
		name := result.name
		if !strings.Contains(path, "enerateContent") {
			name = namesByID[result.id]
		} else {
			valid = assert.Empty(t, result.id) && valid
		}
		valid = assert.Contains(t, declarations, name) && valid
		valid = assert.False(t, seen[name], "both tool results must be correlated once") && valid
		seen[name] = true
		expected := "0"
		if name == "echo_false" {
			expected = "false"
		}
		valid = assert.JSONEq(t, `{"result":`+expected+"}", result.payload) && valid
	}
	return valid
}

func gatewayCloudVertexToolResponse(body io.Reader) ([]byte, error) {
	// Vertex native tool calls have no ID field, unlike the Gemini developer
	// API fixture reused above. Keep its native wire shape in this matrix.
	var result bytes.Buffer
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), "data: ") {
			continue
		}
		var response map[string]any
		if err := common.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &response); err != nil {
			return nil, err
		}
		candidates, _ := response["candidates"].([]any)
		for _, candidate := range candidates {
			value, _ := candidate.(map[string]any)
			content, _ := value["content"].(map[string]any)
			parts, _ := content["parts"].([]any)
			for _, part := range parts {
				value, _ := part.(map[string]any)
				if call, ok := value["functionCall"].(map[string]any); ok {
					delete(call, "id")
				}
			}
		}
		encoded, err := common.Marshal(response)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&result, "data: %s\n\n", encoded)
	}
	return result.Bytes(), scanner.Err()
}

func validateGatewayCloudToolRequest(t *testing.T, body []byte, responsePath string, withResult bool) bool {
	t.Helper()
	request := gjson.ParseBytes(body)
	var toolName, callID, name, args, resultID, result string
	switch {
	case responsePath == "/v1/responses":
		toolName = request.Get("tools.0.name").String()
		for _, item := range request.Get("input").Array() {
			switch item.Get("type").String() {
			case "function_call":
				callID, name, args = item.Get("call_id").String(), item.Get("name").String(), item.Get("arguments").String()
			case "function_call_output":
				resultID, result = item.Get("call_id").String(), item.Get("output").String()
			}
		}
	case responsePath == "/v1/messages":
		toolName = request.Get("tools.0.name").String()
		for _, message := range request.Get("messages").Array() {
			for _, block := range message.Get("content").Array() {
				switch block.Get("type").String() {
				case "tool_use":
					callID, name, args = block.Get("id").String(), block.Get("name").String(), block.Get("input").Raw
				case "tool_result":
					resultID = block.Get("tool_use_id").String()
					content := block.Get("content")
					result = content.String()
					if content.IsArray() {
						result = content.Get("0.text").String()
					}
				}
			}
		}
	case strings.Contains(responsePath, "GenerateContent") || strings.Contains(responsePath, "generateContent"):
		toolName = request.Get("tools.0.functionDeclarations.0.name").String()
		for _, content := range request.Get("contents").Array() {
			for _, part := range content.Get("parts").Array() {
				if call := part.Get("functionCall"); call.Exists() {
					callID, name, args = call.Get("id").String(), call.Get("name").String(), call.Get("args").Raw
				}
				if response := part.Get("functionResponse"); response.Exists() {
					resultID = response.Get("id").String()
					result = response.Get("response").Raw
				}
			}
		}
	default:
		toolName = request.Get("tools.0.function.name").String()
		for _, message := range request.Get("messages").Array() {
			for _, call := range message.Get("tool_calls").Array() {
				callID, name, args = call.Get("id").String(), call.Get("function.name").String(), call.Get("function.arguments").String()
			}
			if message.Get("role").String() == "tool" {
				resultID, result = message.Get("tool_call_id").String(), message.Get("content").String()
			}
		}
	}
	valid := assert.Equal(t, "echo", toolName, "the native adapter request must retain the tool declaration")
	if !withResult {
		return valid
	}
	if strings.Contains(responsePath, "enerateContent") {
		valid = assert.Empty(t, callID, "Vertex native tool history correlates by name") && valid
		valid = assert.Empty(t, resultID, "Vertex native tool results have no ID field") && valid
	} else {
		valid = assert.Equal(t, "call_sdk", callID, "the native tool history must retain the provider call ID") && valid
		valid = assert.Equal(t, callID, resultID, "the native result must refer to its matching call") && valid
	}
	valid = assert.Equal(t, "echo", name) && valid
	valid = assert.JSONEq(t, `{"value":"example"}`, args) && valid
	return assert.JSONEq(t, `{"result":"done"}`, result, "the native result payload must retain its structured value") && valid
}

func TestGatewayGeminiAWSDeferredResponse(t *testing.T) {
	gatewayURL, key, _ := setupGatewaySDK(t)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id <> ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{
		"type": constant.ChannelTypeAws, "key": "isolated-token|us-east-1",
		"model_mapping": `{"gpt-4o-mini":"claude-3-5-sonnet-20240620"}`,
	}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	client := service.GetHttpClient()
	original := client.Transport
	t.Cleanup(func() { client.Transport = original })
	var sends atomic.Int32
	client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
		sends.Add(1)
		assert.Contains(t, request.URL.Path, "anthropic.claude-3-5-sonnet-20240620-v1:0/invoke")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request,
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_deferred","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`))}, nil
	})
	request, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1beta/models/gpt-4o-mini:generateContent",
		strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":32}}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err, "deferred AWS execution must not panic on its empty initial HTTP response")
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Equal(t, "hello", gjson.GetBytes(body, "candidates.0.content.parts.0.text").String())
	assert.EqualValues(t, 12, gjson.GetBytes(body, "usageMetadata.totalTokenCount").Int())
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayVertexToolIdentityRejectedBeforeSending(t *testing.T) {
	gatewayURL, key, _ := setupGatewaySDK(t)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id <> ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{
		"type": constant.ChannelTypeVertexAi, "key": "isolated-vertex-key", "settings": `{"vertex_key_type":"api_key"}`,
		"model_mapping": `{"gpt-4o-mini":"gemini-2.5-flash"}`,
	}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	var owner model.Token
	require.NoError(t, model.DB.Where("key = ?", key).First(&owner).Error)
	client := service.GetHttpClient()
	original := client.Transport
	t.Cleanup(func() { client.Transport = original })
	var sends atomic.Int32
	client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
		sends.Add(1)
		return nil, fmt.Errorf("ambiguous history reached provider transport")
	})
	var native dto.GeminiChatRequest
	require.NoError(t, common.Unmarshal([]byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"id":"call_a","name":"echo","args":{}}},{"functionCall":{"id":"call_b","name":"echo","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"call_b","name":"echo","response":{"value":false}}},{"functionResponse":{"id":"call_a","name":"echo","response":{"value":0}}}]}],"generationConfig":{"maxOutputTokens":32}}`), &native))
	for _, policy := range []string{"safe", "strict", "allow"} {
		token := &model.Token{UserId: owner.UserId, Key: "vertexprojection" + policy, Name: policy, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true, ConversionPolicy: policy}
		require.NoError(t, model.DB.Create(token).Error)
		for _, tc := range []struct {
			format types.RelayFormat
			path   string
		}{
			{types.RelayFormatOpenAI, "/v1/chat/completions"},
			{types.RelayFormatOpenAIResponses, "/v1/responses"},
			{types.RelayFormatClaude, "/v1/messages"},
			{types.RelayFormatGemini, "/v1beta/models/gpt-4o-mini:generateContent"},
		} {
			result, err := relayconvert.ConvertRequest(context.Background(), &convmeta.Values{UpstreamModelName: "gpt-4o-mini", ChannelMetaAttached: true}, tc.format, &native)
			require.NoError(t, err)
			body, err := common.Marshal(result.Value)
			require.NoError(t, err)
			request, err := http.NewRequest(http.MethodPost, gatewayURL+tc.path, bytes.NewReader(body))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+token.Key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, response.StatusCode, "%s/%s: %s", policy, tc.format, data)
			assert.Contains(t, string(data), "tool history becomes ambiguous")
		}
	}
	assert.Zero(t, sends.Load())
	var user model.User
	require.NoError(t, model.DB.First(&user, owner.UserId).Error)
	assert.Equal(t, 10000000, user.Quota)
	var billingCount int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&billingCount).Error)
	assert.Zero(t, billingCount, "rejected tool histories must not create a billing session")
}
