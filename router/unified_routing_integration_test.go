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
			name: "compact base transition", path: "/v1/responses/compact", models: "gpt-5-openai-compact,gpt-5",
			body:             `{"model":"gpt-5","input":"hello"}`,
			upstreamResponse: `{"id":"cmp-test","object":"response.compaction","model":"gpt-5","output":[{"type":"compaction","id":"cmp-item","encrypted_content":"test"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
			channelType:      constant.ChannelTypeOpenAI, quota: 1000, calls: 2,
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
			channelType:      constant.ChannelTypeSora, quota: 4000, calls: 1,
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
				&model.BillingRecovery{}, &model.BillingRecoveryAdjustment{},
				&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{},
				&model.Log{}, &model.Task{}, &model.Midjourney{},
			))
			previousRDB, previousMemory := common.RDB, common.MemoryCacheEnabled
			previousPrices := ratio_setting.ModelPrice2JSONString()
			client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
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
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(priceJSON)))
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if test.calls == 2 && n == 1 {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"message":"The requested model does not exist","type":"invalid_request_error","code":"model_not_found","param":"model"}}`)
					return
				}
				_, _ = io.WriteString(w, test.upstreamResponse)
			}))
			defer upstream.Close()
			user := &model.User{Username: "routing-smoke", Password: "test-password", Status: common.UserStatusEnabled, Group: "retired-denied-group", Quota: 1_000_000}
			require.NoError(t, db.Create(user).Error)
			token := &model.Token{UserId: user.Id, Key: "routingsmokelocalkey", Name: "smoke", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1_000_000}
			require.NoError(t, db.Create(token).Error)
			channel := &model.Channel{Type: test.channelType, Name: "local upstream", Status: common.ChannelStatusEnabled, Key: "test-upstream-key", BaseURL: &upstream.URL, Models: test.models, Group: "unrelated-channel-group", CapacityTotal: 1, ChannelRatio: 2}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, channel.AddAbilities(db))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			SetVideoRouter(engine)
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token.Key)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Equal(t, int32(test.calls), calls.Load(), response.Body.String())
			require.NoError(t, db.First(user, user.Id).Error)
			assert.Equal(t, test.quota, user.UsedQuota)
			assert.Equal(t, int64(0), client.ZCard(context.Background(), service.ChannelRouteLeaseKey(channel.Id)).Val())
			if test.name == "chat" {
				held, _, err := service.AcquireConfiguredRouteLease(context.Background(), "occupied", channel.Id, user.Id, token.Id, test.models, time.Minute)
				require.NoError(t, err)
				for _, failure := range []string{"capacity", "redis"} {
					if failure == "redis" {
						require.NoError(t, service.ReleaseConfiguredRouteLease(context.Background(), held))
						common.RedisEnabled = false
					}
					blocked := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
					blocked.Header.Set("Content-Type", "application/json")
					blocked.Header.Set("Authorization", "Bearer "+token.Key)
					blockedResponse := httptest.NewRecorder()
					engine.ServeHTTP(blockedResponse, blocked)
					assert.Equal(t, http.StatusServiceUnavailable, blockedResponse.Code, failure)
					assert.Equal(t, int32(1), calls.Load(), failure)
					require.NoError(t, db.First(user, user.Id).Error)
					assert.Equal(t, test.quota, user.UsedQuota, failure)
				}
			}
			if test.name == "suno" || test.name == "video" {
				var task model.Task
				require.NoError(t, db.First(&task).Error)
				assert.Equal(t, channel.Id, task.ChannelId)
				require.NotNil(t, task.PrivateData.BillingContext)
				assert.Equal(t, 2.0, task.PrivateData.BillingContext.GroupRatio)
			}
		})
	}
}
