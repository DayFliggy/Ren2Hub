package router

import (
	"bytes"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const semanticPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6dWQAAAAASUVORK5CYII="
const semanticSignature = "c2lnbmF0dXJlLXNlbWFudGlj"
const semanticFileURI = "https://generativelanguage.googleapis.com/v1beta/files/semantic-local"

type gatewaySemanticCase struct {
	Label    string         `json:"label"`
	Protocol string         `json:"protocol"`
	Feature  string         `json:"feature"`
	Action   string         `json:"action"`
	Policy   string         `json:"policy"`
	Stream   bool           `json:"stream"`
	Reject   bool           `json:"reject"`
	Foreign  bool           `json:"foreign"`
	Body     map[string]any `json:"body"`
	Expected map[string]any `json:"-"`
}

func TestGatewayOfficialSemanticPythonSDK(t *testing.T) {
	runGatewaySemanticSDK(t, "GATEWAY_SDK_PYTHON", "verify_semantics.py")
}

func TestGatewayOfficialSemanticNodeSDK(t *testing.T) {
	runGatewaySemanticSDK(t, "GATEWAY_SDK_NODE", "verify_semantics.mjs")
}

func runGatewaySemanticSDK(t *testing.T, runtime, script string) {
	binary := os.Getenv(runtime)
	if binary == "" {
		t.Skip(runtime + " is required")
	}
	cases := gatewaySemanticCases()
	if script == "verify_semantics.py" {
		for index := range cases {
			test := &cases[index]
			if test.Protocol == "gemini" && test.Feature == "search" && !test.Reject {
				// Pin the installed SDK's actual nested interval serialization.
				test.Expected["tools"] = []any{map[string]any{"googleSearch": map[string]any{"timeRangeFilter": map[string]any{"start_time": "2026-01-01T00:00:00+00:00", "end_time": "2026-02-01T00:00:00+00:00"}}}}
			}
		}
	}
	var lock sync.Mutex
	sends := make(map[string]int)
	gateway, key, foreign := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var test *gatewaySemanticCase
		for index := range cases {
			if bytes.Contains(body, []byte(`"`+cases[index].Label+`"`)) {
				test = &cases[index]
				break
			}
		}
		if !assert.NotNil(t, test, "every SDK request must carry its scenario label") {
			http.Error(w, "missing scenario", http.StatusBadRequest)
			return
		}
		lock.Lock()
		sends[test.Label]++
		lock.Unlock()
		if !assert.False(t, test.Reject, "%s reached upstream", test.Label) {
			http.Error(w, "rejected semantic request was sent", http.StatusBadRequest)
			return
		}
		for path, expected := range test.Expected {
			actual := gjson.GetBytes(body, path)
			if path == "contents.0.parts.1.inlineData.data" {
				encoded := strings.NewReplacer("-", "+", "_", "/").Replace(actual.String())
				decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
				require.NoError(t, decodeErr)
				want, decodeErr := base64.StdEncoding.DecodeString(semanticPNG)
				require.NoError(t, decodeErr)
				assert.Equal(t, want, decoded, "native Gemini must retain identical image bytes")
				continue
			}
			want, marshalErr := common.Marshal(expected)
			require.NoError(t, marshalErr)
			assert.JSONEq(t, string(want), actual.Raw, "%s: %s", test.Label, path)
		}
		if test.Feature == "state" || test.Feature == "private_file" {
			gatewaySemanticStateResponse(w, test.Protocol, test.Feature)
			return
		}
		recorder := httptest.NewRecorder()
		r.Body = io.NopCloser(bytes.NewReader(body))
		gatewaySDKUpstream(recorder, r)
		for name, values := range recorder.Header() {
			w.Header()[name] = values
		}
		responseBody := recorder.Body.Bytes()
		if test.Feature == "structured" {
			responseBody = bytes.ReplaceAll(responseBody, []byte(`"hello"`), []byte(`"{\"value\":\"hello\"}"`))
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(responseBody)
	})
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	base := channel.GetBaseURL()
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id > ?", 0).Update("status", common.ChannelStatusManuallyDisabled).Error)
	prices := map[string]float64{}
	channelIDs := map[string]int{}
	for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
		for _, route := range []string{"native", "converted"} {
			channelType := constant.ChannelTypeOpenAI
			if protocol == "anthropic" {
				channelType = constant.ChannelTypeAnthropic
			} else if protocol == "gemini" {
				channelType = constant.ChannelTypeGemini
			}
			if route == "converted" {
				channelType = constant.ChannelTypeOpenAI
				if protocol == "chat" || protocol == "responses" {
					channelType = constant.ChannelTypeAnthropic
				}
			}
			name := "semantic-" + protocol + "-" + route
			item := &model.Channel{Name: name, Models: name, Type: channelType, Key: "semantic-local-key", Status: common.ChannelStatusEnabled,
				BaseURL: &base, Group: "default", CapacityTotal: 10, ChannelRatio: "1"}
			require.NoError(t, model.DB.Create(item).Error)
			require.NoError(t, item.AddAbilities(model.DB))
			channelIDs[name], prices[name] = item.Id, 0.001
		}
	}
	priceJSON, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, configureGatewayFixturePrices(string(priceJSON), false))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 1000000}).Error)
	strict := &model.Token{UserId: 1, Key: "semanticstrictlocal", Name: "semantic strict", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true, ConversionPolicy: "strict"}
	require.NoError(t, model.DB.Create(strict).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	// File upload is outside this gateway's product surface; seed only ownership
	// metadata, then let official SDKs exercise the existing file reference field.
	chatChannel, err := model.GetChannelById(channelIDs["semantic-chat-native"], true)
	require.NoError(t, err)
	credential, err := model.EnsureGatewayCredential(context.Background(), chatChannel.Id, chatChannel.Key)
	require.NoError(t, err)
	endpoint, err := url.Parse(base + "/v1/chat/completions")
	require.NoError(t, err)
	transport, err := common.Marshal(struct {
		URL     string            `json:"url"`
		Host    string            `json:"host"`
		Headers map[string]string `json:"headers"`
	}{endpoint.String(), endpoint.Host, map[string]string{}})
	require.NoError(t, err)
	require.NoError(t, model.BindGatewayState(context.Background(), model.GatewayStateBinding{UserID: 1, TokenID: 1, Kind: "file", ChannelID: chatChannel.Id,
		CredentialID: credential.ID, EndpointID: chatChannel.GatewayEndpointID(), TransportID: model.GatewayDigest(string(transport))}, "file-semantic-local"))
	observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var user model.User
		var token model.Token
		var logs int64
		var billingSessions int64
		require.NoError(t, model.DB.First(&user, 1).Error)
		require.NoError(t, model.DB.First(&token, 1).Error)
		require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
		require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&billingSessions).Error)
		lock.Lock()
		defer lock.Unlock()
		total := 0
		for _, count := range sends {
			total += count
		}
		data, marshalErr := common.Marshal(map[string]any{"sends": total, "quota": user.Quota, "token": token.RemainQuota, "logs": logs, "billing_sessions": billingSessions})
		require.NoError(t, marshalErr)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(observer.Close)
	configuration, err := common.Marshal(cases)
	require.NoError(t, err)
	configurationPath := filepath.Join(t.TempDir(), "semantic-cases.json")
	require.NoError(t, os.WriteFile(configurationPath, configuration, 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, filepath.Join("..", "tests", "gateway-sdk", script))
	command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+gateway, "GATEWAY_SDK_KEY="+key, "GATEWAY_SDK_FOREIGN_KEY="+foreign,
		"GATEWAY_SEMANTIC_STRICT_KEY="+strict.Key, "GATEWAY_SEMANTIC_CASES="+configurationPath, "GATEWAY_SEMANTIC_OBSERVER="+observer.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	require.NoError(t, err)
	awaitGatewayPermitsReleased(t)
	for _, test := range cases {
		expected := 1
		if test.Reject {
			expected = 0
		}
		assert.Equal(t, expected, sends[test.Label], test.Label)
	}
	var unhealthy int64
	require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&unhealthy).Error)
	assert.Zero(t, unhealthy)
}

func gatewaySemanticCases() []gatewaySemanticCase {
	var cases []gatewaySemanticCase
	schema := map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false}
	for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
		add := func(feature, action, policy string, stream, rejected bool) {
			label := fmt.Sprintf("sem-%s-%s-%s-%s-%t", protocol, feature, action, policy, stream)
			route := "native"
			if action == "converted" {
				route = action
			}
			name := "semantic-" + protocol + "-" + route
			body := map[string]any{"model": name, "max_tokens": 32, "messages": []any{map[string]any{"role": "user", "content": label}}}
			if protocol == "responses" {
				body = map[string]any{"model": name, "max_output_tokens": 32, "input": label}
			} else if protocol == "gemini" {
				body = map[string]any{"model": name, "contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": label}}}}, "config": map[string]any{"maxOutputTokens": 32}}
			}
			test := gatewaySemanticCase{Label: label, Protocol: protocol, Feature: feature, Action: action, Policy: policy, Stream: stream, Reject: rejected, Foreign: action == "foreign", Body: body, Expected: map[string]any{}}
			switch feature {
			case "vision":
				uri := "data:image/png;base64," + semanticPNG
				part := map[string]any{"type": "image_url", "image_url": map[string]any{"url": uri}}
				target := protocol
				if route == "converted" {
					target = "chat"
					if protocol == "chat" || protocol == "responses" {
						target = "anthropic"
					}
				}
				switch protocol {
				case "responses":
					body["input"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": label}, map[string]any{"type": "input_image", "image_url": uri}}}}
				case "anthropic":
					part = map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": semanticPNG}}
					fallthrough
				case "chat":
					body["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": label}, part}}}
				case "gemini":
					body["contents"] = []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": label}, map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": semanticPNG}}}}}
				}
				switch target {
				case "chat":
					test.Expected["messages.0.content.1.image_url.url"] = uri
				case "responses":
					test.Expected["input.0.content.1.image_url"] = uri
				case "anthropic":
					test.Expected["messages.0.content.1.source.data"] = semanticPNG
					test.Expected["messages.0.content.1.source.media_type"] = "image/png"
				case "gemini":
					test.Expected["contents.0.parts.1.inlineData.data"] = semanticPNG
					test.Expected["contents.0.parts.1.inlineData.mimeType"] = "image/png"
				}
			case "structured":
				format := map[string]any{"type": "json_schema", "name": "result", "strict": true, "schema": schema}
				switch protocol {
				case "chat":
					body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "result", "strict": true, "schema": schema}}
					test.Expected["response_format"] = body["response_format"]
				case "responses":
					body["text"] = map[string]any{"format": format}
					test.Expected["text"] = body["text"]
				case "anthropic":
					body["output_config"] = map[string]any{"format": map[string]any{"type": "json_schema", "schema": schema}}
					test.Expected["output_config"] = body["output_config"]
				case "gemini":
					config := body["config"].(map[string]any)
					config["responseMimeType"], config["responseJsonSchema"] = "application/json", schema
					test.Expected["generationConfig.responseJsonSchema"] = schema
				}
			case "search":
				switch protocol {
				case "chat":
					body["web_search_options"] = map[string]any{"search_context_size": "high"}
					test.Expected["web_search_options"] = body["web_search_options"]
				case "responses":
					body["tools"] = []any{map[string]any{"type": "web_search", "filters": map[string]any{"allowed_domains": []string{"example.com"}}}}
					test.Expected["tools"] = body["tools"]
				case "anthropic":
					body["tools"] = []any{map[string]any{"type": "web_search_20250305", "name": "web_search", "max_uses": 1}}
					test.Expected["tools"] = body["tools"]
				case "gemini":
					body["config"].(map[string]any)["tools"] = []any{map[string]any{"googleSearch": map[string]any{"timeRangeFilter": map[string]any{"startTime": "2026-01-01T00:00:00Z", "endTime": "2026-02-01T00:00:00Z"}}}}
					test.Expected["tools"] = body["config"].(map[string]any)["tools"]
				}
			case "custom":
				body["tools"] = []any{map[string]any{"type": "custom", "name": "shell", "format": map[string]any{"type": "text"}}}
				test.Expected["tools"] = body["tools"]
			case "state", "private_file":
				if action != "seed" {
					switch protocol {
					case "chat":
						test.Expected["messages.0.content.1.file.file_id"] = "file-semantic-local"
					case "responses":
						test.Expected["input.0.encrypted_content"] = "semantic-encrypted"
					case "anthropic":
						test.Expected["messages.0.content.0.signature"] = semanticSignature
					case "gemini":
						path, value := "contents.0.parts.0.thoughtSignature", semanticSignature
						if feature == "private_file" {
							path, value = "contents.0.parts.0.fileData.fileUri", semanticFileURI
						}
						test.Expected[path] = value
					}
				}
			}
			cases = append(cases, test)
		}
		for _, route := range []string{"native", "converted"} {
			for _, stream := range []bool{false, true} {
				add("vision", route, "safe", stream, false)
			}
		}
		add("structured", "native", "safe", false, false)
		add("structured", "converted", "safe", false, true)
		for _, feature := range []string{"search", "custom"} {
			if feature == "custom" && protocol != "responses" {
				continue
			}
			add(feature, "native", "safe", false, false)
			for _, policy := range []string{"safe", "strict"} {
				add(feature, "converted", policy, false, true)
			}
		}
		for _, feature := range []string{"state", "private_file"} {
			if feature == "private_file" && protocol != "gemini" {
				continue
			}
			if protocol != "chat" {
				add(feature, "seed", "safe", false, false)
			}
			add(feature, "continue", "safe", false, false)
			add(feature, "unknown", "safe", false, true)
			add(feature, "foreign", "safe", false, true)
		}
	}
	return cases
}

func gatewaySemanticStateResponse(w http.ResponseWriter, protocol, feature string) {
	w.Header().Set("Content-Type", "application/json")
	switch protocol {
	case "responses":
		_, _ = io.WriteString(w, `{"id":"resp-semantic","object":"response","created_at":1,"status":"completed","output":[{"id":"reason-semantic","type":"reasoning","summary":[],"encrypted_content":"semantic-encrypted"},{"id":"msg-semantic","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`)
	case "anthropic":
		_, _ = fmt.Fprintf(w, `{"id":"msg-semantic","type":"message","role":"assistant","model":"semantic-anthropic-native","content":[{"type":"thinking","thinking":"reason","signature":%q},{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`, semanticSignature)
	case "gemini":
		part := map[string]any{"text": "hello", "thoughtSignature": semanticSignature}
		parts := []any{part}
		if feature == "private_file" {
			parts = []any{map[string]any{"fileData": map[string]any{"mimeType": "image/png", "fileUri": semanticFileURI}}, map[string]any{"text": "hello"}}
		}
		data, _ := common.Marshal(map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 2, "totalTokenCount": 12}})
		_, _ = w.Write(data)
	default:
		_, _ = io.WriteString(w, `{"id":"chat-semantic","object":"chat.completion","created":1,"model":"semantic-chat-native","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	}
}
