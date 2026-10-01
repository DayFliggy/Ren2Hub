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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type gatewayCompatibleProvider struct {
	Name, Model, Upstream, Key string
	ChannelType, APIType       int
	Definition                 relaycapability.Definition
}

func gatewayCompatibleProviders(t *testing.T) []gatewayCompatibleProvider {
	t.Helper()
	var providers []gatewayCompatibleProvider
	channelTypes := make([]int, 0, len(constant.ChannelTypeNames))
	for channelType := range constant.ChannelTypeNames {
		channelTypes = append(channelTypes, channelType)
	}
	slices.Sort(channelTypes)
	for _, channelType := range channelTypes {
		apiType, registered := common.ChannelType2APIType(channelType)
		if !registered || channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeAzure {
			continue
		}
		switch apiType {
		case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
			constant.APITypeNewAPI, constant.APITypeSub2API, constant.APITypeDeepSeek,
			constant.APITypeAli, constant.APITypeMoonshot, constant.APITypeMiniMax,
			constant.APITypeSiliconFlow, constant.APITypeVolcEngine, constant.APITypePerplexity,
			constant.APITypeXai, constant.APITypeBaiduV2, constant.APITypeMistral,
			constant.APITypeZhipuV4, constant.APITypeCloudflare, constant.APITypeSubmodel, constant.APITypeTencent:
		default:
			continue
		}
		name := strings.ReplaceAll(strings.ToLower(constant.GetChannelTypeName(channelType)), " ", "")
		key, upstream := "isolated-compatible-"+name, "upstream-"+name
		definition, found := relaycapability.DescribeForCredentials(apiType, upstream, []string{key})
		require.True(t, found)
		providers = append(providers, gatewayCompatibleProvider{Name: name, Model: "compatible-" + name, Upstream: upstream, Key: key, ChannelType: channelType, APIType: apiType, Definition: definition})
	}
	require.Len(t, providers, 28, "update the explicit adapter coverage when the registry changes")
	return providers
}

func TestGatewayOfficialCompatibleSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("GATEWAY_SDK_PYTHON and GATEWAY_SDK_NODE must point to installed SDK runtimes")
	}
	protocols := map[string]types.RelayFormat{"chat": types.RelayFormatOpenAI, "responses": types.RelayFormatOpenAIResponses, "anthropic": types.RelayFormatClaude, "gemini": types.RelayFormatGemini}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_compatible.py"}, {node, "verify_compatible.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			providers := gatewayCompatibleProviders(t)
			gatewayURL, key, _ := setupGatewaySDK(t)
			client := service.GetHttpClient()
			originalTransport := client.Transport
			t.Cleanup(func() { client.Transport = originalTransport })
			counts := make(map[string]int)
			var lock sync.Mutex
			client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
				for _, provider := range providers {
					if request.URL.Host != provider.Model+".invalid" {
						continue
					}
					body, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					for protocol, format := range protocols {
						for _, mode := range []string{"json", "stream"} {
							label := provider.Model + "-" + protocol + "-" + mode
							if !bytes.Contains(body, []byte(label)) {
								continue
							}
							lock.Lock()
							counts[label]++
							lock.Unlock()
							surface, supported := provider.Definition.Resolve(format)
							valid := assert.True(t, supported, "%s must be rejected before sending", label)
							valid = assert.Equal(t, http.MethodPost, request.Method, label) && valid
							valid = assert.Equal(t, gatewayCompatiblePath(provider, surface.Target, mode == "stream"), request.URL.Path, label) && valid
							valid = assert.Equal(t, "Bearer "+provider.Key, request.Header.Get("Authorization"), label) && valid
							if surface.Target == types.RelayFormatGemini {
								valid = assert.NotEmpty(t, gjson.GetBytes(body, "contents").Array(), label) && valid
								valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "generationConfig.maxOutputTokens").Int(), label) && valid
							} else {
								valid = assert.Equal(t, provider.Upstream, gjson.GetBytes(body, "model").String(), label) && valid
								valid = assert.Equal(t, mode == "stream", gjson.GetBytes(body, "stream").Bool(), label) && valid
								if surface.Target == types.RelayFormatOpenAIResponses {
									valid = assert.True(t, gjson.GetBytes(body, "input").Exists(), label) && valid
									valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "max_output_tokens").Int(), label) && valid
								} else {
									valid = assert.NotEmpty(t, gjson.GetBytes(body, "messages").Array(), label) && valid
									valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "max_tokens").Int(), label) && valid
								}
							}
							if provider.APIType == constant.APITypeNewAPI || provider.APIType == constant.APITypeSub2API {
								if format == types.RelayFormatClaude {
									valid = assert.Equal(t, provider.Key, request.Header.Get("x-api-key"), label) && valid
									valid = assert.NotEmpty(t, request.Header.Get("anthropic-version"), label) && valid
								} else if format == types.RelayFormatGemini {
									valid = assert.Equal(t, provider.Key, request.Header.Get("x-goog-api-key"), label) && valid
								}
							}
							if !valid {
								return nil, fmt.Errorf("invalid compatible SDK fixture request for %s", label)
							}
							fixturePath := map[types.RelayFormat]string{types.RelayFormatOpenAI: "/v1/chat/completions", types.RelayFormatOpenAIResponses: "/v1/responses", types.RelayFormatClaude: "/v1/messages", types.RelayFormatGemini: "/v1beta/models/fixture:generateContent"}[surface.Target]
							if surface.Target == types.RelayFormatGemini && mode == "stream" {
								fixturePath = strings.Replace(fixturePath, "generateContent", "streamGenerateContent", 1)
							}
							recorder := httptest.NewRecorder()
							gatewaySDKUpstream(recorder, httptest.NewRequest(http.MethodPost, fixturePath, bytes.NewReader(body)))
							response := recorder.Result()
							response.Request = request
							// State IDs belong to distinct upstream accounts in this fixture.
							payload := strings.NewReplacer("resp_fixture", "resp_"+label, "msg_response_fixture", "msg_"+label).Replace(recorder.Body.String())
							response.Body = io.NopCloser(strings.NewReader(payload))
							response.ContentLength = int64(len(payload))
							return response, nil
						}
					}
					return nil, fmt.Errorf("unidentified compatible SDK fixture request")
				}
				return nil, fmt.Errorf("unexpected destination blocked by compatible SDK fixture")
			})
			prices := map[string]float64{}
			var config []map[string]any
			for _, provider := range providers {
				prices[provider.Model] = 0.001
				mapping, err := common.Marshal(map[string]string{provider.Model: provider.Upstream})
				require.NoError(t, err)
				mappingJSON, endpoint := string(mapping), "https://"+provider.Model+".invalid"
				if provider.ChannelType == constant.ChannelTypeCustom {
					endpoint += "/custom/{model}"
				}
				configured := &model.Channel{Type: provider.ChannelType, Name: "SDK compatible fixture", Models: provider.Model, Key: provider.Key, Status: common.ChannelStatusEnabled, BaseURL: &endpoint, Other: "fixture-account", Group: "default", CapacityTotal: 10, ChannelRatio: "1", ModelMapping: &mappingJSON}
				require.NoError(t, model.DB.Create(configured).Error)
				require.NoError(t, configured.AddAbilities(model.DB))
				supported := map[string]bool{}
				for protocol, format := range protocols {
					surface, found := provider.Definition.Resolve(format)
					supported[protocol] = found
					if found {
						assert.True(t, surface.Stream, provider.Name)
					}
				}
				config = append(config, map[string]any{"name": provider.Name, "model": provider.Model, "supported": supported})
			}
			priceJSON, err := common.Marshal(prices)
			require.NoError(t, err)
			require.NoError(t, configureGatewayFixturePrices(string(priceJSON), false))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			configJSON, err := common.Marshal(config)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+gatewayURL, "GATEWAY_SDK_KEY="+key, "GATEWAY_COMPATIBLE_CONFIG="+string(configJSON))
			output, err := command.CombinedOutput()
			assert.NoError(t, err, "%s", output)
			t.Log(string(output))
			lock.Lock()
			defer lock.Unlock()
			for _, provider := range providers {
				for protocol, format := range protocols {
					_, supported := provider.Definition.Resolve(format)
					want := 0
					if supported {
						want = 1
					}
					for _, mode := range []string{"json", "stream"} {
						label := provider.Model + "-" + protocol + "-" + mode
						assert.Equal(t, want, counts[label], label+" actual upstream sends")
					}
				}
			}
		})
	}
}

func gatewayCompatiblePath(provider gatewayCompatibleProvider, target types.RelayFormat, stream bool) string {
	if provider.ChannelType == constant.ChannelTypeCustom {
		return "/custom/" + provider.Upstream
	}
	if target == types.RelayFormatGemini {
		action := "generateContent"
		if stream {
			action = "streamGenerateContent"
		}
		return "/v1beta/models/" + provider.Upstream + ":" + action
	}
	path := map[types.RelayFormat]string{types.RelayFormatOpenAI: "/v1/chat/completions", types.RelayFormatOpenAIResponses: "/v1/responses", types.RelayFormatClaude: "/v1/messages"}[target]
	switch provider.APIType {
	case constant.APITypeAli:
		if target == types.RelayFormatOpenAIResponses {
			return "/api/v2/apps/protocols/compatible-mode" + path
		}
		return "/compatible-mode" + path
	case constant.APITypeMoonshot, constant.APITypeMiniMax, constant.APITypeDeepSeek:
		if target == types.RelayFormatClaude {
			return "/anthropic" + path
		}
		if provider.APIType == constant.APITypeMiniMax {
			return "/v1/text/chatcompletion_v2"
		}
		if provider.APIType == constant.APITypeDeepSeek && target == types.RelayFormatOpenAIResponses {
			return "/responses"
		}
	case constant.APITypeVolcEngine:
		return "/api/v3" + strings.TrimPrefix(path, "/v1")
	case constant.APITypePerplexity:
		if target == types.RelayFormatOpenAI {
			return "/chat/completions"
		}
	case constant.APITypeBaiduV2:
		return "/v2/chat/completions"
	case constant.APITypeZhipuV4:
		if target == types.RelayFormatClaude {
			return "/api/anthropic" + path
		}
		return "/api/paas/v4/chat/completions"
	case constant.APITypeCloudflare:
		return "/client/v4/accounts/fixture-account/ai" + path
	}
	return path
}

func TestGatewayCompatibleInvalidUsageIsNotDeliveredOrCharged(t *testing.T) {
	for _, provider := range []struct {
		name string
		kind int
	}{{"openai", constant.ChannelTypeOpenAI}, {"cloudflare", constant.ChannelCloudflare}} {
		for _, invalidUsage := range []struct {
			name, body string
		}{
			{"negative", `{"prompt_tokens":-1,"completion_tokens":2,"total_tokens":1}`},
			{"contradictory", `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":11}`},
		} {
			t.Run(provider.name+"/"+invalidUsage.name, func(t *testing.T) {
				var sends atomic.Int32
				gatewayURL, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					var user model.User
					require.NoError(t, model.DB.First(&user, 1).Error)
					assert.EqualValues(t, 9999500, user.Quota, "precharge must precede provider send")
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"id":"invalid-usage","object":"chat.completion","model":"compatible-usage","choices":[{"index":0,"message":{"role":"assistant","content":"must-not-be-delivered"},"finish_reason":"stop"}],"usage":%s}`, invalidUsage.body)
				})
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Type, channel.Models, channel.Other = provider.kind, "compatible-usage", "isolated-account"
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "other": channel.Other}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
				require.NoError(t, configureGatewayFixturePrices(`{"compatible-usage":0.001}`, false))
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 2000}).Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				status, output := postGatewayTask(t, gatewayURL, key, "/v1/chat/completions", `{"model":"compatible-usage","messages":[{"role":"user","content":"hello"}],"max_tokens":32}`)
				assert.Equal(t, http.StatusBadGateway, status, output)
				assert.NotContains(t, output, "must-not-be-delivered")
				assert.EqualValues(t, 1, sends.Load())
				awaitGatewayPermitsReleased(t)
				// Invalid usage is rejected before delivery, so the reservation
				// is released instead of charged or held.
				var user model.User
				var token model.Token
				require.Eventually(t, func() bool {
					return model.DB.First(&user, 1).Error == nil && model.DB.First(&token, 1).Error == nil && user.Quota == 10000000 && token.RemainQuota == 2000 && user.ReservedQuota == 0 && token.ReservedQuota == 0
				}, time.Second, 10*time.Millisecond)
				var recovery model.BillingRecovery
				require.NoError(t, model.DB.First(&recovery).Error)
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				assert.Zero(t, user.UsedQuota)
				assert.Zero(t, user.RequestCount)
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				assert.Empty(t, logs)
				var health []model.ChannelHealth
				require.NoError(t, model.DB.Where("failure_count > 0").Find(&health).Error)
				require.NotEmpty(t, health)
				for _, scope := range health {
					assert.Equal(t, 1, scope.ChannelID)
					assert.Equal(t, 1, scope.FailureCount)
				}
			})
		}
	}
}

type gatewayCompatibleFailureWriter struct {
	gin.ResponseWriter
	mode string
}

func (writer *gatewayCompatibleFailureWriter) Write(body []byte) (int, error) {
	switch writer.mode {
	case "short_write":
		return writer.ResponseWriter.Write(body[:len(body)/2])
	case "cancelled":
		return 0, context.Canceled
	default:
		return 0, io.ErrClosedPipe
	}
}

func TestGatewayCompatibleJSONClientFailureReleasesWithoutHealthPenalty(t *testing.T) {
	for _, provider := range []struct {
		name string
		kind int
	}{{"openai", constant.ChannelTypeOpenAI}, {"cloudflare", constant.ChannelCloudflare}} {
		for _, mode := range []string{"short_write", "cancelled", "write_error"} {
			t.Run(provider.name+"/"+mode, func(t *testing.T) {
				var sends atomic.Int32
				_, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					gatewaySDKUpstream(w, r)
				})
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Type, channel.Models, channel.Other = provider.kind, "compatible-usage", "isolated-account"
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "other": channel.Other}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
				require.NoError(t, configureGatewayFixturePrices(`{"compatible-usage":0.001}`, false))
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 2000}).Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				engine := gin.New()
				engine.Use(middleware.RequestId(), func(c *gin.Context) {
					c.Writer = &gatewayCompatibleFailureWriter{ResponseWriter: c.Writer, mode: mode}
					c.Next()
				})
				SetRelayRouter(engine)
				request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"compatible-usage","messages":[{"role":"user","content":"hello"}],"max_tokens":32}`))
				request.Header.Set("Authorization", "Bearer "+key)
				request.Header.Set("Content-Type", "application/json")
				engine.ServeHTTP(httptest.NewRecorder(), request)
				assert.EqualValues(t, 1, sends.Load(), "a client write failure must not retry the provider")
				awaitGatewayPermitsReleased(t)
				// The response never completed delivery to the client, so no
				// valid output exists and the reservation is released.
				var user model.User
				var token model.Token
				require.Eventually(t, func() bool {
					return model.DB.First(&user, 1).Error == nil && model.DB.First(&token, 1).Error == nil && user.Quota == 10000000 && token.RemainQuota == 2000 && user.ReservedQuota == 0 && token.ReservedQuota == 0
				}, time.Second, 10*time.Millisecond)
				var recovery model.BillingRecovery
				require.NoError(t, model.DB.First(&recovery).Error)
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				assert.Zero(t, user.UsedQuota)
				assert.Zero(t, user.RequestCount)
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				assert.Empty(t, logs)
				var failures int64
				require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
				assert.Zero(t, failures, "client delivery errors must not penalize the provider")
			})
		}
	}
}
