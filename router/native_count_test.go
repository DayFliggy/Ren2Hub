package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func nativeCountFixture(t *testing.T, channelType int, upstream string) (*gin.Engine, model.User, model.Token, model.Channel) {
	t.Helper()
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Log{}))
	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })
	service.InitHttpClient()
	require.NoError(t, i18n.Init())
	user := model.User{Username: "count-user", Status: common.UserStatusEnabled, Quota: 1000000, MaxConcurrency: 1}
	require.NoError(t, model.DB.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "nativecounttestkey", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000}
	require.NoError(t, model.DB.Create(&token).Error)
	channel := model.Channel{Type: channelType, Key: "provider-key", Name: "count-channel", Status: common.ChannelStatusEnabled, Models: "gemini-2.0-flash,claude-sonnet-4", BaseURL: &upstream, ChannelRatio: "1", MaxConcurrency: 1}
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	engine := gin.New()
	SetRelayRouter(engine)
	return engine, user, token, channel
}

func nativeCountRequest(engine *gin.Engine, path, body, key string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	engine.ServeHTTP(recorder, req)
	return recorder
}

func TestNativeCountProtocolsUseProviderCountWithoutBilling(t *testing.T) {
	tests := []struct {
		name, path, body, response, mapped, expectedPath string
		provider                                         int
	}{
		{"Claude", "/v1/messages/count_tokens?beta=true", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}],"system":"instruction","tools":[{"name":"search","input_schema":{"type":"object","properties":{"file_id":{"type":"string"}}}}]}`, `{"input_tokens":37}`, "claude-sonnet-4-20250514", "/v1/messages/count_tokens", constant.ChannelTypeAnthropic},
		{"Gemini v1", "/v1/models/gemini-2.0-flash:countTokens?alt=json", `{"contents":[{"parts":[{"text":"hello"},{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}]}`, `{"totalTokens":17,"totalBillableCharacters":5,"promptTokensDetails":[{"modality":"TEXT","tokenCount":17}]}`, "gemini-2.5-flash", "/v1beta/models/gemini-2.5-flash:countTokens", constant.ChannelTypeGemini},
		{"Gemini beta nested", "/v1beta/models/gemini-2.0-flash:countTokens", `{"generateContentRequest":{"model":"models/gemini-2.0-flash","contents":[{"parts":[{"text":"hello"}]}],"systemInstruction":{"parts":[{"text":"instruction"}]},"tools":[{"functionDeclarations":[{"name":"search"}]}]}}`, `{"totalTokens":0}`, "gemini-2.5-flash", "/v1beta/models/gemini-2.5-flash:countTokens", constant.ChannelTypeGemini},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, tc.expectedPath, r.URL.Path)
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Cookie"))
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				var expectedBody map[string]any
				require.NoError(t, common.Unmarshal([]byte(tc.body), &expectedBody))
				if tc.provider == constant.ChannelTypeAnthropic {
					expectedBody["model"] = tc.mapped
				} else if nested, ok := expectedBody["generateContentRequest"].(map[string]any); ok {
					nested["model"] = "models/" + tc.mapped
				}
				expectedJSON, err := common.Marshal(expectedBody)
				require.NoError(t, err)
				require.JSONEq(t, string(expectedJSON), string(data), "native fields must survive without generation conversion")
				if tc.provider == constant.ChannelTypeAnthropic {
					require.Equal(t, "provider-key", r.Header.Get("x-api-key"))
					require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
					require.Equal(t, "true", r.URL.Query().Get("beta"))
					require.Contains(t, string(data), tc.mapped)
					require.NotContains(t, string(data), "max_tokens")
					require.Contains(t, string(data), "instruction")
				} else {
					require.Equal(t, "provider-key", r.Header.Get("x-goog-api-key"))
					require.Empty(t, r.URL.RawQuery)
					if strings.Contains(tc.body, "generateContentRequest") {
						require.Contains(t, string(data), "models/"+tc.mapped)
					}
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer upstream.Close()
			engine, user, token, ch := nativeCountFixture(t, tc.provider, upstream.URL)
			mapping := `{"gemini-2.0-flash":"gemini-2.5-flash","claude-sonnet-4":"claude-sonnet-4-20250514"}`
			require.NoError(t, model.DB.Model(&ch).Update("model_mapping", mapping).Error)
			for range 2 {
				result := nativeCountRequest(engine, tc.path, tc.body, token.Key)
				require.Equal(t, 200, result.Code, result.Body.String())
				require.JSONEq(t, tc.response, result.Body.String())
				require.Zero(t, service.ChannelInFlight(ch.Id))
			}
			require.EqualValues(t, 2, calls.Load())
			var actualUser model.User
			var actualToken model.Token
			var actualChannel model.Channel
			require.NoError(t, model.DB.First(&actualUser, user.Id).Error)
			require.NoError(t, model.DB.First(&actualToken, token.Id).Error)
			require.NoError(t, model.DB.First(&actualChannel, ch.Id).Error)
			require.Equal(t, user.Quota, actualUser.Quota)
			require.Zero(t, actualUser.UsedQuota)
			require.Zero(t, actualUser.RequestCount)
			require.Equal(t, token.RemainQuota, actualToken.RemainQuota)
			require.Zero(t, actualToken.UsedQuota)
			require.Zero(t, actualChannel.UsedQuota)
			var logs int64
			require.NoError(t, model.DB.Model(&model.Log{}).Count(&logs).Error)
			require.Zero(t, logs)
		})
	}
}

func TestNativeCountRejectsBadRequestsBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"totalTokens":1}`))
	}))
	defer upstream.Close()
	engine, _, token, _ := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	for _, version := range []string{"v1", "v1beta"} {
		for _, input := range []struct{ suffix, body string }{
			{"gemini-2.0-flash:unknown", `{"contents":[{"parts":[{"text":"x"}]}]}`},
			{"gemini-2.0-flash:countTokens", `{"model":"other","contents":[{"parts":[{"text":"x"}]}]}`},
			{"gemini-2.0-flash:countTokens", `{"contents":[]}`},
			{"gemini-2.0-flash:countTokens", `{"contents":[{}]}`},
			{"gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"fileData":{"fileUri":"gs://private"}}]}]}`},
			{"gemini-2.0-flash:countTokens", `{"generateContentRequest":{"model":"models/other","contents":[{"parts":[{"text":"x"}]}]}}`},
			{"gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"x"}]}],"generateContentRequest":{}}`},
			{"gemini-2.0-flash:countTokens", `{"generateContentRequest":{"cachedContent":"cachedContents/private","contents":[{"parts":[{"text":"x"}]}]}}`},
			{"gemini%2Fother:countTokens", `{}`}, {"gemini%252Fother:countTokens", `{}`}, {"gemini%3Fother:countTokens", `{}`}, {"gemini%23other:countTokens", `{}`},
			{"gemini-2.0-flash:countTokens", `{"contents":`},
		} {
			path := "/" + version + "/models/" + input.suffix
			result := nativeCountRequest(engine, path, input.body, token.Key)
			require.Equal(t, 400, result.Code, path+" "+result.Body.String())
		}
	}
	require.Zero(t, calls.Load())
}

func TestNativeCountUpstreamErrorsNeverEstimate(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		status, expected int
	}{
		{"missing", "{}", 200, 502}, {"negative", `{"totalTokens":-1}`, 200, 502}, {"fraction", `{"totalTokens":1.2}`, 200, 502}, {"string", `{"totalTokens":"5"}`, 200, 502}, {"null", `{"totalTokens":null}`, 200, 502}, {"bad JSON", "no", 200, 502}, {"overflow", `{"totalTokens":9223372036854775808}`, 200, 502}, {"oversized", strings.Repeat("x", (1<<20)+1), 200, 502},
		{"bad request", `{"error":{"message":"invalid prompt"}}`, 400, 400}, {"unauthorized", `{"error":{"message":"bad credential"}}`, 401, 401}, {"forbidden", `{"error":{"message":"denied"}}`, 403, 403}, {"missing endpoint", `{"error":{"message":"unsupported"}}`, 404, 404}, {"limited", `{"error":{"message":"rate limited"}}`, 429, 429}, {"server", `{"error":{"message":"unavailable"}}`, 503, 503}, {"HTML error", `<html>unavailable</html>`, 502, 502}, {"redirect", "", 302, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "9")
				w.Header().Set("Location", "https://must-not-follow.invalid")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			engine, user, token, ch := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
			result := nativeCountRequest(engine, "/v1beta/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, token.Key)
			require.Equal(t, tc.expected, result.Code, result.Body.String())
			require.NotContains(t, result.Body.String(), `"totalTokens":0`)
			require.Zero(t, service.ChannelInFlight(ch.Id))
			if tc.status >= 400 && strings.HasPrefix(tc.body, "{") {
				// The provider message is kept in the generation path's error
				// shape; masking is covered by the dedicated error test.
				var payload struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, common.Unmarshal(result.Body.Bytes(), &payload))
				var upstreamPayload struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, common.Unmarshal([]byte(tc.body), &upstreamPayload))
				require.Equal(t, upstreamPayload.Error.Message, payload.Error.Message)
				require.Equal(t, "9", result.Header().Get("Retry-After"))
			}
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			require.Equal(t, user.Quota, after.Quota)
			require.Zero(t, after.UsedQuota)
		})
	}
}

func TestNativeCountAuthorizationAndModelPermissions(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"totalTokens":1}`))
	}))
	defer upstream.Close()
	engine, _, token, _ := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	path := "/v1/models/gemini-2.0-flash:countTokens"
	body := `{"contents":[{"parts":[{"text":"hello"}]}]}`
	require.Equal(t, 401, nativeCountRequest(engine, path, body, "").Code)
	require.NoError(t, model.DB.Model(&token).Update("status", common.TokenStatusDisabled).Error)
	require.Equal(t, 401, nativeCountRequest(engine, path, body, token.Key).Code)
	require.NoError(t, model.DB.Model(&token).Updates(map[string]any{"status": common.TokenStatusEnabled, "model_limits_enabled": true, "model_limits": "other"}).Error)
	require.Equal(t, 403, nativeCountRequest(engine, path, body, token.Key).Code)
	require.Zero(t, calls.Load())
}

func TestNativeCountConcurrencyRejectsImmediatelyAndReleases(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"totalTokens":1}`))
	}))
	defer upstream.Close()
	engine, _, token, ch := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	path := "/v1/models/gemini-2.0-flash:countTokens"
	body := `{"contents":[{"parts":[{"text":"hello"}]}]}`
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() { finished <- nativeCountRequest(engine, path, body, token.Key) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream did not start")
	}
	require.Equal(t, 1, service.ChannelInFlight(ch.Id))
	result := nativeCountRequest(engine, path, body, token.Key)
	require.Equal(t, 429, result.Code, result.Body.String())
	close(release)
	require.Equal(t, 200, (<-finished).Code)
	require.Zero(t, service.ChannelInFlight(ch.Id))
	require.Equal(t, 200, nativeCountRequest(engine, path, body, token.Key).Code)
}

func TestNativeCountDeadlineReturnsErrorAndReleases(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer upstream.Close()
	defer close(release)
	engine, _, token, ch := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	recorder := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/models/gemini-2.0-flash:countTokens", strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.Key)
	engine.ServeHTTP(recorder, req)
	require.Equal(t, 504, recorder.Code, recorder.Body.String())
	require.Zero(t, service.ChannelInFlight(ch.Id))
}

func TestNativeCountUsesCurrentRequestRateLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"totalTokens":1}`)) }))
	defer upstream.Close()
	engine, _, token, _ := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	oldEnabled, oldTotal, oldSuccess := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitCount = 1
	setting.ModelRequestRateLimitSuccessCount = 0
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = oldEnabled
		setting.ModelRequestRateLimitCount = oldTotal
		setting.ModelRequestRateLimitSuccessCount = oldSuccess
	})
	path := "/v1/models/gemini-2.0-flash:countTokens"
	body := `{"contents":[{"parts":[{"text":"hello"}]}]}`
	require.Equal(t, 200, nativeCountRequest(engine, path, body, token.Key).Code)
	require.Equal(t, 429, nativeCountRequest(engine, path, body, token.Key).Code)
}

func TestNativeCountReusesManualRoutesAndRejectsUnsupportedChannels(t *testing.T) {
	var firstCalls, secondCalls, unsupportedCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		_, _ = w.Write([]byte(`{"totalTokens":1}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		_, _ = w.Write([]byte(`{"totalTokens":2}`))
	}))
	defer second.Close()
	unsupported := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unsupportedCalls.Add(1)
		_, _ = w.Write([]byte(`{"totalTokens":3}`))
	}))
	defer unsupported.Close()
	engine, user, token, firstChannel := nativeCountFixture(t, constant.ChannelTypeGemini, first.URL)
	secondURL, unsupportedURL := second.URL, unsupported.URL
	secondChannel := model.Channel{Type: constant.ChannelTypeGemini, Key: "second-key", Name: "manual-count", Status: common.ChannelStatusEnabled, Models: "gemini-2.0-flash", BaseURL: &secondURL, ChannelRatio: "1", MaxConcurrency: 1}
	unsupportedChannel := model.Channel{Type: constant.ChannelTypeOpenAI, Key: "unsupported", Name: "no-native-count", Status: common.ChannelStatusEnabled, Models: "gemini-2.0-flash", BaseURL: &unsupportedURL, ChannelRatio: "0.001", MaxConcurrency: 1}
	for _, ch := range []*model.Channel{&secondChannel, &unsupportedChannel} {
		require.NoError(t, model.DB.Create(ch).Error)
		require.NoError(t, ch.AddAbilities(nil))
	}
	path, body := "/v1/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`
	result := nativeCountRequest(engine, path, body, token.Key)
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Zero(t, unsupportedCalls.Load(), "auto routing must filter unsupported providers before ranking")

	allowed := map[int]bool{firstChannel.Id: true, secondChannel.Id: true, unsupportedChannel.Id: true}
	routing, err := model.SaveTokenRouting(context.Background(), user.Id, token.Id, model.TokenRouting{
		TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual},
		Entries:           []model.TokenRouteEntry{{ChannelID: secondChannel.Id, Position: 0, Enabled: true}, {ChannelID: firstChannel.Id, Position: 1, Enabled: true}},
	}, allowed)
	require.NoError(t, err)
	before := secondCalls.Load()
	result = nativeCountRequest(engine, path, body, token.Key)
	require.Equal(t, 200, result.Code, result.Body.String())
	require.JSONEq(t, `{"totalTokens":2}`, result.Body.String())
	require.Equal(t, before+1, secondCalls.Load())
	_, err = model.SaveTokenRouting(context.Background(), user.Id, token.Id, model.TokenRouting{
		TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual, Version: routing.Version},
		Entries:           []model.TokenRouteEntry{{ChannelID: unsupportedChannel.Id, Position: 0, Enabled: true}},
	}, allowed)
	require.NoError(t, err)
	before = firstCalls.Load() + secondCalls.Load()
	result = nativeCountRequest(engine, path, body, token.Key)
	require.GreaterOrEqual(t, result.Code, 400, result.Body.String())
	require.Equal(t, before, firstCalls.Load()+secondCalls.Load(), "manual routes cannot fall back outside the selected list")
	require.Zero(t, unsupportedCalls.Load())
}

func TestNativeCountReusesEnabledMultiKeyAndProxyTransport(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "count-provider.invalid", r.URL.Host)
		require.Equal(t, "/v1beta/models/gemini-2.0-flash:countTokens", r.URL.Path)
		require.Equal(t, "enabled-key", r.Header.Get("x-goog-api-key"))
		_, _ = w.Write([]byte(`{"totalTokens":8}`))
	}))
	defer proxy.Close()
	engine, _, token, ch := nativeCountFixture(t, constant.ChannelTypeGemini, "http://count-provider.invalid")
	ch.SetSetting(kitdto.ChannelSettings{Proxy: proxy.URL})
	info := model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling, MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled}}
	require.NoError(t, model.DB.Model(&ch).Updates(map[string]any{"key": "disabled-key\nenabled-key", "channel_info": info, "setting": ch.Setting}).Error)
	for range 3 {
		result := nativeCountRequest(engine, "/v1beta/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, token.Key)
		require.Equal(t, 200, result.Code, result.Body.String())
	}
	require.EqualValues(t, 3, calls.Load())
}

func TestNativeCountPreservesNativeAuthenticationHeaders(t *testing.T) {
	for _, provider := range []int{constant.ChannelTypeAnthropic, constant.ChannelTypeGemini} {
		t.Run(map[int]string{constant.ChannelTypeAnthropic: "Claude", constant.ChannelTypeGemini: "Gemini"}[provider], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if provider == constant.ChannelTypeAnthropic {
					require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
					require.Equal(t, "prompt-caching-2024-07-31", r.Header.Get("anthropic-beta"))
					_, _ = w.Write([]byte(`{"input_tokens":4}`))
				} else {
					_, _ = w.Write([]byte(`{"totalTokens":4}`))
				}
			}))
			defer upstream.Close()
			engine, _, token, _ := nativeCountFixture(t, provider, upstream.URL)
			path, body, header := "/v1/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, "x-goog-api-key"
			if provider == constant.ChannelTypeAnthropic {
				path, body, header = "/v1/messages/count_tokens", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`, "x-api-key"
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(header, token.Key)
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, req)
			require.Equal(t, 200, recorder.Code, recorder.Body.String())
		})
	}
}

func TestNativeCountRejectsGenerationAndMalformedClaudeFields(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"input_tokens":1}`))
	}))
	defer upstream.Close()
	engine, _, token, _ := nativeCountFixture(t, constant.ChannelTypeAnthropic, upstream.URL)
	for _, body := range []string{
		`{"model":"claude-sonnet-4"}`,
		`{"model":"claude-sonnet-4","messages":[]}`,
		`{"model":"claude-sonnet-4","messages":[null]}`,
		`{"model":"claude-sonnet-4","messages":[{"role":"system","content":"hello"}]}`,
		`{"model":"claude-sonnet-4","messages":[{"role":"user","content":null}]}`,
		`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"max_tokens":4}`,
		`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":true}`,
		`{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"private"}}]}]}`,
	} {
		result := nativeCountRequest(engine, "/v1/messages/count_tokens", body, token.Key)
		require.Equal(t, 400, result.Code, body+" "+result.Body.String())
	}
	require.Zero(t, calls.Load())
}

func TestNativeCountRetainsSharedRequestSizeLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("oversized request reached provider") }))
	defer upstream.Close()
	engine, _, token, _ := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	original := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = original })
	body := `{"contents":[{"parts":[{"text":"` + strings.Repeat("x", 1<<20) + `"}]}]}`
	result := nativeCountRequest(engine, "/v1/models/gemini-2.0-flash:countTokens", body, token.Key)
	require.Equal(t, 413, result.Code, result.Body.String())
}

func TestNativeCountRejectsUnsafeMappedModelBeforeProvider(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	engine, _, token, ch := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
	for _, mapped := range []string{"../private", "gemini:generateContent", "gemini?alt=sse", "gemini%2fprivate", "gemini#private"} {
		mapping, err := common.Marshal(map[string]string{"gemini-2.0-flash": mapped})
		require.NoError(t, err)
		require.NoError(t, model.DB.Model(&ch).Update("model_mapping", string(mapping)).Error)
		result := nativeCountRequest(engine, "/v1/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, token.Key)
		require.Equal(t, 400, result.Code, mapped+" "+result.Body.String())
		require.Zero(t, service.ChannelInFlight(ch.Id))
	}
	require.Zero(t, calls.Load())
}

func TestNativeCountBrokenUpstreamDoesNotReturnEstimatedSuccess(t *testing.T) {
	for _, disconnected := range []bool{true, false} {
		t.Run(map[bool]string{true: "closed before headers", false: "truncated response"}[disconnected], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if disconnected {
					connection, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					_ = connection.Close()
					return
				}
				w.Header().Set("Content-Length", "100")
				_, _ = w.Write([]byte(`{"totalTokens":5}`))
			}))
			defer upstream.Close()
			engine, user, token, ch := nativeCountFixture(t, constant.ChannelTypeGemini, upstream.URL)
			result := nativeCountRequest(engine, "/v1/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, token.Key)
			require.Equal(t, 502, result.Code, result.Body.String())
			require.NotContains(t, result.Body.String(), "totalTokens")
			require.Zero(t, service.ChannelInFlight(ch.Id))
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			require.Equal(t, user.Quota, after.Quota)
			require.Zero(t, after.UsedQuota)
		})
	}
}

func TestNativeCountAppliesChannelHeaderOverrides(t *testing.T) {
	for _, provider := range []int{constant.ChannelTypeAnthropic, constant.ChannelTypeGemini} {
		t.Run(map[int]string{constant.ChannelTypeAnthropic: "Claude", constant.ChannelTypeGemini: "Gemini"}[provider], func(t *testing.T) {
			header := "x-goog-api-key"
			path, body, response := "/v1/models/gemini-2.0-flash:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, `{"totalTokens":4}`
			if provider == constant.ChannelTypeAnthropic {
				header = "x-api-key"
				path, body, response = "/v1/messages/count_tokens", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`, `{"input_tokens":4}`
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "required", r.Header.Get("X-Channel-Gate"))
				require.Equal(t, "count.example.invalid", r.Host)
				require.Equal(t, "overridden-provider-key", r.Header.Get(header), "channel override must take precedence over native headers")
				require.Equal(t, "Bearer provider-key", r.Header.Get("X-Provider-Token"))
				require.Equal(t, "client-metadata", r.Header.Get("X-Client-Metadata"))
				require.Empty(t, r.Header.Get("Authorization"), "wildcard passthrough must not expose local credentials")
				require.Empty(t, r.Header.Get("Cookie"))
				_, _ = w.Write([]byte(response))
			}))
			defer upstream.Close()
			engine, _, token, ch := nativeCountFixture(t, provider, upstream.URL)
			overrides, err := common.Marshal(map[string]any{
				"*":                 true,
				"X-Channel-Gate":    "required",
				"Host":              "count.example.invalid",
				header:              "overridden-{api_key}",
				"X-Provider-Token":  "Bearer {api_key}",
				"X-Client-Metadata": "{client_header:X-Caller-Metadata}",
			})
			require.NoError(t, err)
			require.NoError(t, model.DB.Model(&ch).Update("header_override", string(overrides)).Error)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token.Key)
			req.Header.Set("Cookie", "session=local-secret")
			req.Header.Set("X-Caller-Metadata", "client-metadata")
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.EqualValues(t, 1, calls.Load())
			require.Zero(t, service.ChannelInFlight(ch.Id))

			require.NoError(t, model.DB.Model(&ch).Update("header_override", `{"X-Channel-Gate":"{client_header:}"}`).Error)
			result := nativeCountRequest(engine, path, body, token.Key)
			require.Equal(t, http.StatusInternalServerError, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), "invalid_channel_configuration")
			require.EqualValues(t, 1, calls.Load(), "invalid header configuration must fail before upstream dispatch")
			require.Zero(t, service.ChannelInFlight(ch.Id))
		})
	}
}

func TestNativeCountDistinguishesToolDataFromProviderReferences(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		provider   int
		private    bool
	}{
		{"Gemini tool response JSON", `{"contents":[{"parts":[{"functionResponse":{"name":"get_report","response":{"file_id":"user-report-42","fileData":{"fileUri":"caller-value"},"cachedContent":"caller-value"}}}]}]}`, constant.ChannelTypeGemini, false},
		{"Gemini tool arguments JSON", `{"contents":[{"parts":[{"functionCall":{"name":"get_report","args":{"file_id":"user-report-42","content":[{"source":{"type":"file","file_id":"caller-value"}}]}}}]}]}`, constant.ChannelTypeGemini, false},
		{"Gemini nested tool JSON", `{"generateContentRequest":{"contents":[{"parts":[{"functionResponse":{"name":"get_report","response":{"file_id":"user-report-42","sequence":9007199254740993}}}]}],"tools":[{"functionDeclarations":[{"name":"get_report","parameters":{"type":"object","properties":{"fileData":{"type":"string"}}}}]}]}}`, constant.ChannelTypeGemini, false},
		{"Claude tool input JSON", `{"model":"claude-sonnet-4","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"toolu_test","name":"get_report","input":{"file_id":"user-report-42","fileData":{"fileUri":"caller-value"},"cachedContent":"caller-value","content":[{"source":{"type":"file","file_id":"caller-value"}}]}}]}]}`, constant.ChannelTypeAnthropic, false},
		{"Gemini private file", `{"contents":[{"parts":[{"fileData":{"fileUri":"files/private"}}]}]}`, constant.ChannelTypeGemini, true},
		{"Gemini snake private file", `{"contents":[{"parts":[{"file_data":{"file_uri":"files/private"}}]}]}`, constant.ChannelTypeGemini, true},
		{"Gemini system private file", `{"generateContentRequest":{"contents":[{"parts":[{"text":"hello"}]}],"systemInstruction":{"parts":[{"fileData":{"fileUri":"files/private"}}]}}}`, constant.ChannelTypeGemini, true},
		{"Gemini tool response media", `{"contents":[{"parts":[{"functionResponse":{"name":"get_report","response":{},"parts":[{"fileData":{"fileUri":"files/private"}}]}}]}]}`, constant.ChannelTypeGemini, true},
		{"Gemini private cache", `{"generateContentRequest":{"contents":[{"parts":[{"text":"hello"}]}],"cachedContent":"cachedContents/private"}}`, constant.ChannelTypeGemini, true},
		{"Gemini snake private cache", `{"generateContentRequest":{"contents":[{"parts":[{"text":"hello"}]}],"cached_content":"cachedContents/private"}}`, constant.ChannelTypeGemini, true},
		{"Claude private image", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"private"}}]}]}`, constant.ChannelTypeAnthropic, true},
		{"Claude private document in tool result", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_test","content":[{"type":"document","source":{"type":"file","file_id":"private"}}]}]}]}`, constant.ChannelTypeAnthropic, true},
		{"Claude private upload block", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"container_upload","file_id":"private"}]}]}`, constant.ChannelTypeAnthropic, true},
		{"Claude private file in document content", `{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"document","source":{"type":"content","content":[{"type":"image","source":{"type":"file","file_id":"private"}}]}}]}]}`, constant.ChannelTypeAnthropic, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, response := "/v1/models/gemini-2.0-flash:countTokens", `{"totalTokens":4}`
			if tc.provider == constant.ChannelTypeAnthropic {
				path, response = "/v1/messages/count_tokens", `{"input_tokens":4}`
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if strings.Contains(tc.body, "generateContentRequest") {
					require.JSONEq(t, strings.Replace(tc.body, `"generateContentRequest":{`, `"generateContentRequest":{"model":"models/gemini-2.0-flash",`, 1), string(data))
				} else {
					require.JSONEq(t, tc.body, string(data), "user tool JSON must survive unchanged")
				}
				if strings.Contains(tc.body, "9007199254740993") {
					require.Contains(t, string(data), "9007199254740993", "native tool values must not be rounded by interface decoding")
				}
				_, _ = w.Write([]byte(response))
			}))
			defer upstream.Close()
			engine, _, token, ch := nativeCountFixture(t, tc.provider, upstream.URL)
			result := nativeCountRequest(engine, path, tc.body, token.Key)
			if tc.private {
				require.Equal(t, http.StatusBadRequest, result.Code, result.Body.String())
				require.Contains(t, result.Body.String(), "provider-owned file or cache")
				require.Zero(t, calls.Load(), "provider references require an ownership contract before dispatch")
			} else {
				require.Equal(t, http.StatusOK, result.Code, result.Body.String())
				require.EqualValues(t, 1, calls.Load())
			}
			require.Zero(t, service.ChannelInFlight(ch.Id))
		})
	}
}
