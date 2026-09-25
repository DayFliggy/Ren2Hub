package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedRoutingPreservesRelayProtocolsAndChannelBilling(t *testing.T) {
	service.InitHttpClient()
	for _, test := range []struct {
		name, path, models, body, upstreamResponse string
		channelType, quota, calls                  int
	}{
		{
			name: "chat", path: "/v1/chat/completions", models: "gpt-4o-mini",
			body:             `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`,
			upstreamResponse: `{"id":"chat-test","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"test response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			channelType:      constant.ChannelTypeOpenAI, quota: 1000, calls: 1,
		},
		{
			name: "compact ordinary mapping", path: "/v1/responses/compact", models: "gpt-5",
			body:             `{"model":"gpt-5","input":"hello"}`,
			upstreamResponse: `{"id":"cmp-test","object":"response.compaction","model":"gpt-5","output":[{"type":"compaction","id":"cmp-item","encrypted_content":"test"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
			channelType:      constant.ChannelTypeOpenAI, quota: 1000, calls: 1,
		},
		{
			name: "suno", path: "/suno/submit/music", models: "suno_music",
			body:             `{"prompt":"test melody","mv":"chirp-v3-0"}`,
			upstreamResponse: `{"code":"success","message":"","data":"upstream-suno-task"}`,
			channelType:      constant.ChannelTypeSunoAPI, quota: 1000, calls: 1,
		},
		{
			name: "video", path: "/v1/videos", models: "sora-2",
			body:             `{"model":"sora-2","prompt":"test scene","seconds":"4","size":"720x1280"}`,
			upstreamResponse: `{"id":"upstream-video-task","object":"video","model":"sora-2","status":"queued","seconds":"4","size":"720x1280"}`,
			channelType:      constant.ChannelTypeSora, quota: 1000, calls: 1,
		},
		{
			name: "midjourney", path: "/mj/submit/imagine", models: "mj_imagine",
			body:             `{"prompt":"test scene"}`,
			upstreamResponse: `{"code":1,"description":"submitted","result":"upstream-mj-task"}`,
			channelType:      constant.ChannelTypeMidjourney, quota: 1000, calls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupRelayRouterTestDB(t)
			db := model.DB
			require.NoError(t, db.AutoMigrate(
				&model.UserRouteGroup{}, &model.UserRouteEntry{}, &model.RoutePolicy{},
				&model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}, &model.ChannelHealth{},
				&model.BillingRecovery{}, &model.BillingEvent{},
				&model.Log{}, &model.QuotaData{}, &model.Task{}, &model.Midjourney{},
				&model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{},
			))
			require.NoError(t, controller.InitTaskPlugins())
			previousRDB, previousMemory := common.RDB, common.MemoryCacheEnabled
			previousPrices := ratio_setting.ModelPrice2JSONString()
			redisServer := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: redisServer.Addr(), MaxRetries: -1})
			common.RDB, common.RedisEnabled, common.MemoryCacheEnabled = client, true, false
			t.Cleanup(func() {
				common.RDB, common.MemoryCacheEnabled = previousRDB, previousMemory
				require.NoError(t, client.Close())
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
			})
			prices := map[string]float64{}
			for _, name := range strings.Split(test.models, ",") {
				prices[name] = .001
			}
			priceJSON, err := common.Marshal(prices)
			require.NoError(t, err)
			require.NoError(t, publishGatewayFixturePrices(string(priceJSON), false))
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if test.name == "compact ordinary mapping" {
					var body map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &body))
					assert.Equal(t, "mapped-gpt-5", body["model"])
					assert.Equal(t, "/v1/responses/compact", r.URL.Path)
				}
				_, _ = io.WriteString(w, test.upstreamResponse)
			}))
			defer upstream.Close()
			user := &model.User{Username: "routing-smoke", Password: "test-password", Status: common.UserStatusEnabled, Group: "retired-denied-group", Quota: 1_000_000}
			require.NoError(t, db.Create(user).Error)
			token := &model.Token{UserId: user.Id, Key: "routingsmokelocalkey", Name: "smoke", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1_000_000}
			require.NoError(t, db.Create(token).Error)
			channel := &model.Channel{Type: test.channelType, Name: "local upstream", Status: common.ChannelStatusEnabled, Key: "test-upstream-key", BaseURL: &upstream.URL, Models: test.models, Group: "unrelated-channel-group", CapacityTotal: 1, ChannelRatio: 2}
			if test.name == "compact ordinary mapping" {
				mapping := `{"gpt-5":"mapped-gpt-5"}`
				channel.ModelMapping = &mapping
			}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, channel.AddAbilities(db))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			SetVideoRouter(engine)
			engine.NoRoute(SetPluginRouter(engine))
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token.Key)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Equal(t, int32(test.calls), calls.Load(), response.Body.String())
			require.NoError(t, db.First(user, user.Id).Error)
			if test.name == "suno" || test.name == "video" || test.name == "midjourney" {
				assert.Zero(t, user.UsedQuota)
				assert.EqualValues(t, test.quota, user.ReservedQuota)
			} else {
				assert.EqualValues(t, test.quota, user.UsedQuota)
			}
			assert.Equal(t, int64(0), service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(channel.Id)).InFlight)
			if test.name == "chat" {
				held, _, err := service.AcquireGatewayChannelPermit(context.Background(), "occupied", channel.Id, false, nil)
				require.NoError(t, err)
				for _, failure := range []string{"capacity", "redis"} {
					if failure == "redis" {
						held.Release()
						redisServer.Close()
					}
					blocked := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
					blocked.Header.Set("Content-Type", "application/json")
					blocked.Header.Set("Authorization", "Bearer "+token.Key)
					blockedResponse := httptest.NewRecorder()
					engine.ServeHTTP(blockedResponse, blocked)
					want := http.StatusTooManyRequests
					if failure == "redis" {
						want = http.StatusServiceUnavailable
					}
					assert.Equal(t, want, blockedResponse.Code, failure)
					assert.Equal(t, int32(1), calls.Load(), failure)
					require.NoError(t, db.First(user, user.Id).Error)
					assert.EqualValues(t, test.quota, user.UsedQuota, failure)
				}
			}
			if test.name == "suno" || test.name == "video" {
				var task model.Task
				require.NoError(t, db.First(&task).Error)
				assert.Equal(t, channel.Id, task.ChannelId)
				require.NotNil(t, task.PrivateData.BillingContext)
				require.NotNil(t, task.PrivateData.BillingContext.Quote)
				assert.Equal(t, "2", task.PrivateData.BillingContext.Quote.ChannelRatio)
			}
		})
	}
}
