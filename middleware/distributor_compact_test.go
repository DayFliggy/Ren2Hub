package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributeCompactAndNativeResponsesUseUnifiedRouting(t *testing.T) {
	for _, test := range []struct {
		name, path, model, body string
		channelType, status     int
		native                  bool
	}{
		{"ordinary model", "/v1/responses/compact", "gpt-5", `{"model":"gpt-5","input":"hello"}`, constant.ChannelTypeOpenAI, 204, false},
		{"no suffix fallback", "/v1/responses/compact", "gpt-5-openai-compact", `{"model":"gpt-5","input":"hello"}`, constant.ChannelTypeOpenAI, 503, false},
		{"literal suffix", "/v1/responses", "gpt-5-openai-compact", `{"model":"gpt-5-openai-compact","input":"hello"}`, constant.ChannelTypeOpenAI, 204, false},
		{"non GPT ordinary", "/v1/responses/compact", "claude-3-5-sonnet", `{"model":"claude-3-5-sonnet","input":"hello"}`, constant.ChannelTypeOpenAI, 204, false},
		{"suffix is not stripped", "/v1/responses/compact", "claude-3-5-sonnet", `{"model":"claude-3-5-sonnet-openai-compact","input":"hello"}`, constant.ChannelTypeOpenAI, 503, false},
		{"opaque state rejects conversion", "/v1/responses", "gpt-5", `{"model":"gpt-5","input":[{"type":"compaction_trigger"}]}`, constant.ChannelTypeGemini, 503, false},
		{"native compaction", "/v1/responses", "gpt-5", `{"model":"gpt-5","context_management":{"compact_threshold":1000},"input":"hello"}`, constant.ChannelTypeOpenAI, 204, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousDB, previousRedis, previousCache, previousRDB := model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.RDB
			client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.RDB = db, true, false, client
			t.Cleanup(func() {
				model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.RDB = previousDB, previousRedis, previousCache, previousRDB
				require.NoError(t, client.Close())
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.UserChannelEntitlement{}, &model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}, &model.ChannelHealth{}))
			require.NoError(t, db.Create(&model.User{Id: 1, Username: "fixture", Status: common.UserStatusEnabled}).Error)
			channel := &model.Channel{Id: 1, Type: test.channelType, Status: common.ChannelStatusEnabled, Name: test.name, Key: "test-key", Models: test.model, Group: "retired-group"}
			require.NoError(t, db.Create(channel).Error)
			require.NoError(t, channel.AddAbilities(db))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			called := false
			router := gin.New()
			router.POST(test.path, func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyUserId, 1)
				common.SetContextKey(c, constant.ContextKeyTokenId, 2)
				common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "1")
			}, Distribute(), func(c *gin.Context) {
				called = true
				require.Equal(t, test.native, common.GetContextKeyBool(c, constant.ContextKeyResponsesNativeRequired))
				require.True(t, c.GetBool(service.RouteLiveSelectionRequiredContextKey))
				c.Status(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, test.status, recorder.Code, recorder.Body.String())
			require.Equal(t, test.status == 204, called)
		})
	}
}

func TestRequestRequiresNativeResponsesDetectsCompactionItem(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-5",
		"input":[{"type":"compaction","encrypted_content":"ciphertext"}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	require.True(t, requestRequiresNativeResponses(c))
}
