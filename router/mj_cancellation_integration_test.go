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
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyUnknownAcceptanceCancellation(t *testing.T) {
	service.InitHttpClient()
	for _, test := range []struct {
		name, path, model, body string
	}{
		{name: "imagine", path: "/mj/submit/imagine", model: "mj_imagine", body: `{"prompt":"cancel-safe"}`},
		{name: "swap-face", path: "/mj/insight-face/swap", model: "swap_face", body: `{"sourceBase64":"source","targetBase64":"target"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupRelayRouterTestDB(t)
			db := model.DB
			require.NoError(t, db.AutoMigrate(
				&model.UserRouteGroup{}, &model.UserRouteEntry{}, &model.RoutePolicy{},
				&model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}, &model.ChannelHealth{},
				&model.BillingRecovery{}, &model.BillingEvent{},
				&model.Log{}, &model.Midjourney{},
			))

			previousRedis, previousRedisEnabled := common.RDB, common.RedisEnabled
			redisServer := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			common.RDB, common.RedisEnabled = redisClient, true
			t.Cleanup(func() {
				common.RDB, common.RedisEnabled = previousRedis, previousRedisEnabled
				require.NoError(t, redisClient.Close())
			})
			prices, err := common.Marshal(map[string]float64{test.model: 0.001})
			require.NoError(t, err)
			require.NoError(t, configureGatewayFixturePrices(string(prices), false))

			accepted := make(chan struct{})
			release := make(chan struct{})
			var upstreamCancelled atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(accepted)
				select {
				case <-r.Context().Done():
					upstreamCancelled.Store(true)
				case <-release:
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"code":1,"description":"accepted","result":"accepted-task"}`)
			}))
			defer upstream.Close()

			user := &model.User{Username: "mj-cancel-safe", Password: "test-password", Status: common.UserStatusEnabled, Quota: 1_000_000}
			require.NoError(t, db.Create(user).Error)
			token := &model.Token{UserId: user.Id, Key: "mjcancelsafekey", Name: "cancel-safe", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1_000_000}
			require.NoError(t, db.Create(token).Error)
			channel := &model.Channel{Type: constant.ChannelTypeMidjourney, Name: "cancel-safe-upstream", Status: common.ChannelStatusEnabled, Key: "upstream-key", BaseURL: &upstream.URL, Models: test.model, Group: "default", CapacityTotal: 1, ChannelRatio: "2"}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, channel.AddAbilities(db))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))

			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)).WithContext(requestContext)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token.Key)
			response := httptest.NewRecorder()
			handlerDone := make(chan struct{})
			go func() {
				engine.ServeHTTP(response, request)
				close(handlerDone)
			}()
			select {
			case <-accepted:
			case <-handlerDone:
				t.Fatalf("handler returned before upstream acceptance: %d %s", response.Code, response.Body.String())
			case <-time.After(5 * time.Second):
				t.Fatal("upstream acceptance timed out")
			}
			cancel()
			assert.Eventually(t, upstreamCancelled.Load, 2*time.Second, 10*time.Millisecond)
			close(release)
			select {
			case <-handlerDone:
			case <-time.After(5 * time.Second):
				t.Fatal("relay did not finish after client cancellation")
			}

			assert.True(t, upstreamCancelled.Load())
			assert.Empty(t, response.Body.String(), "client cancellation must not write another error response")
			// The gateway never received a job identity, so the reservation is
			// released and later recovery passes cannot charge it.
			var recovery model.BillingRecovery
			for range 2 {
				require.NoError(t, db.First(&recovery).Error)
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				require.NoError(t, db.First(user, user.Id).Error)
				require.NoError(t, db.First(token, token.Id).Error)
				assert.EqualValues(t, 1_000_000, user.Quota)
				assert.EqualValues(t, 1_000_000, token.RemainQuota)
				assert.Zero(t, user.ReservedQuota)
				assert.Zero(t, token.ReservedQuota)
				_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+3600, 10)
				require.NoError(t, err)
			}
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, token.UsedQuota)
			var taskCount int64
			require.NoError(t, db.Model(&model.Midjourney{}).Count(&taskCount).Error)
			assert.Zero(t, taskCount)
			assert.Equal(t, int64(0), service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(channel.Id)).InFlight)
		})
	}
}
