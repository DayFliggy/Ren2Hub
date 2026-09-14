package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayOfficialCustomSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("SDK runtimes are required")
	}
	protocols := []string{"chat", "responses", "anthropic", "gemini"}
	paths := map[string]string{"chat": "/v1/chat/completions", "responses": "/v1/responses", "anthropic": "/v1/messages", "gemini": "/v1beta/models/{model}:generateContent"}
	upstreamModels := map[string]string{"chat": "gpt-4o-mini", "responses": "gpt-4o-mini", "anthropic": "claude-3-5-sonnet", "gemini": "gemini-2.5-flash"}
	converters := map[string]string{
		"chat/chat":           relayconvert.ConverterNone,
		"responses/responses": relayconvert.ConverterNone,
		"anthropic/anthropic": relayconvert.ConverterNone,
		"gemini/gemini":       relayconvert.ConverterNone,
		"chat/responses":      relayconvert.ConverterOpenAIChatToOpenAIResponses,
		"chat/anthropic":      relayconvert.ConverterOpenAIChatToClaudeMessages,
		"chat/gemini":         relayconvert.ConverterOpenAIChatToGeminiContent,
		"responses/chat":      relayconvert.ConverterOpenAIResponsesToOpenAIChat,
		"responses/gemini":    relayconvert.ConverterOpenAIResponsesToGemini,
		"anthropic/chat":      relayconvert.ConverterClaudeMessagesToOpenAIChat,
		"gemini/chat":         relayconvert.ConverterGeminiContentToOpenAIChat,
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_custom.py"}, {node, "verify_custom.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			var lock sync.Mutex
			counts := make(map[string]int)
			gatewayURL, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					http.Error(w, "invalid fixture request", http.StatusBadRequest)
					return
				}
				var incoming, outgoing, mode, label string
				for _, source := range protocols {
					for _, target := range protocols {
						for _, responseMode := range []string{"json", "stream"} {
							candidate := "custom-" + source + "-" + target + "-" + responseMode
							if bytes.Contains(body, []byte(candidate)) {
								incoming, outgoing, mode, label = source, target, responseMode, candidate
							}
						}
					}
				}
				lock.Lock()
				counts[label]++
				lock.Unlock()
				_, supported := converters[incoming+"/"+outgoing]
				valid := assert.NotEmpty(t, label, "request payload must retain its prompt") && assert.True(t, supported, "unsupported path reached upstream")
				valid = assert.Equal(t, http.MethodPost, r.Method) && valid
				valid = assert.Equal(t, "isolated-tenant", r.Header.Get("X-Tenant")) && valid
				valid = assert.Equal(t, "tenant.custom.example", r.Host) && valid
				valid = assert.Equal(t, "fixed", r.URL.Query().Get("revision")) && valid
				stream := mode == "stream"
				path := "/custom/" + outgoing + "/" + upstreamModels[outgoing]
				responsePath := paths[outgoing]
				switch outgoing {
				case "chat", "responses":
					path += paths[outgoing]
					valid = assert.Equal(t, upstreamModels[outgoing], gjson.GetBytes(body, "model").String()) && valid
					valid = assert.Equal(t, stream, gjson.GetBytes(body, "stream").Bool()) && valid
					field := "messages"
					if outgoing == "responses" {
						field = "input"
					}
					valid = assert.True(t, gjson.GetBytes(body, field).Exists()) && valid
					if outgoing == "chat" {
						valid = assert.Equal(t, "Bearer isolated-custom-key", r.Header.Get("Authorization")) && valid
					} else {
						valid = assert.Empty(t, r.Header.Get("Authorization")) && valid
					}
				case "anthropic":
					path += "/v1/messages"
					valid = assert.Equal(t, upstreamModels[outgoing], gjson.GetBytes(body, "model").String()) && valid
					valid = assert.Equal(t, stream, gjson.GetBytes(body, "stream").Bool()) && valid
					valid = assert.Equal(t, "isolated-custom-key", r.Header.Get("X-Custom-Key")) && valid
					valid = assert.NotEmpty(t, r.Header.Get("Anthropic-Version")) && valid
				case "gemini":
					path += ":generateContent"
					if stream {
						path = strings.Replace(path, ":generateContent", ":streamGenerateContent", 1)
						valid = assert.Equal(t, "sse", r.URL.Query().Get("alt")) && valid
					}
					valid = assert.Equal(t, "isolated-custom-key", r.URL.Query().Get("custom_key")) && valid
					valid = assert.True(t, gjson.GetBytes(body, "contents").Exists()) && valid
					responsePath = "/v1beta/models/" + upstreamModels[outgoing] + ":generateContent"
					if stream {
						responsePath = strings.Replace(responsePath, ":generateContent", ":streamGenerateContent", 1)
					}
				}
				valid = assert.Equal(t, path, r.URL.Path) && valid
				if !valid {
					http.Error(w, "invalid final custom request", http.StatusBadRequest)
					return
				}
				upstreamRequest := r.Clone(r.Context())
				upstreamRequest.URL.Path = responsePath
				upstreamRequest.Body = io.NopCloser(bytes.NewReader(body))
				gatewaySDKUpstream(w, upstreamRequest)
			})
			var existing model.Channel
			require.NoError(t, model.DB.First(&existing).Error)
			prices := map[string]float64{}
			cases := []map[string]any{}
			for _, outgoing := range protocols {
				alias := "custom-" + outgoing
				prices[alias] = 0.001
				mapping, err := common.Marshal(map[string]string{alias: upstreamModels[outgoing]})
				require.NoError(t, err)
				mappingJSON, headers := string(mapping), `{"X-Tenant":"isolated-tenant","Host":"tenant.custom.example"}`
				configured := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, Name: "SDK custom fixture", Models: alias, Key: "isolated-custom-key", Status: common.ChannelStatusEnabled, BaseURL: existing.BaseURL, Group: "default", CapacityTotal: 10, ChannelRatio: 1, ModelMapping: &mappingJSON, HeaderOverride: &headers}
				config := &dto.AdvancedCustomConfig{}
				for _, incoming := range protocols {
					converter, supported := converters[incoming+"/"+outgoing]
					cases = append(cases, map[string]any{"incoming": incoming, "outgoing": outgoing, "model": alias, "supported": supported})
					if !supported {
						continue
					}
					upstreamPath := "/custom/" + outgoing + "/{model}" + paths[outgoing]
					if outgoing == "gemini" {
						upstreamPath = "/custom/gemini/{model}:generateContent"
					}
					route := dto.AdvancedCustomRoute{IncomingPath: paths[incoming], UpstreamPath: upstreamPath + "?revision=fixed", Models: []string{alias}, Converter: converter}
					switch outgoing {
					case "responses":
						route.Auth = &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeNone}
					case "anthropic":
						route.Auth = &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeHeader, Name: "X-Custom-Key", Value: "{api_key}"}
					case "gemini":
						route.Auth = &dto.AdvancedCustomRouteAuth{Type: dto.AdvancedCustomAuthTypeQuery, Name: "custom_key", Value: "{api_key}"}
					}
					config.Routes = append(config.Routes, route)
				}
				require.NoError(t, config.Validate())
				configured.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: config})
				require.NoError(t, model.DB.Create(configured).Error)
				require.NoError(t, configured.AddAbilities(model.DB))
			}
			priceJSON, err := common.Marshal(prices)
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(priceJSON)))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			caseJSON, err := common.Marshal(cases)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+gatewayURL, "GATEWAY_SDK_KEY="+key, "GATEWAY_CUSTOM_CASES="+string(caseJSON))
			output, err := command.CombinedOutput()
			assert.NoError(t, err, "%s", output)
			t.Log(string(output))
			lock.Lock()
			defer lock.Unlock()
			for _, incoming := range protocols {
				for _, outgoing := range protocols {
					for _, mode := range []string{"json", "stream"} {
						label := fmt.Sprintf("custom-%s-%s-%s", incoming, outgoing, mode)
						expected := 0
						if _, supported := converters[incoming+"/"+outgoing]; supported {
							expected = 1
						}
						assert.Equal(t, expected, counts[label], label)
					}
				}
			}
			assert.Zero(t, counts[""], "unidentified outbound requests must not occur")
		})
	}
}
