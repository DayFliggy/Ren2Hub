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
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributeCompactAndNativeResponsesUseUnifiedRouting(t *testing.T) {
	for _, test := range []struct {
		name, path, model, body string
		channelType, status     int
		stage                   relaycommon.CompactAttemptStage
		native                  bool
	}{
		{"regular suffix", "/v1/responses", "gpt-5-openai-compact", `{"model":"gpt-5-openai-compact","input":"hello"}`, constant.ChannelTypeOpenAI, 204, relaycommon.CompactAttemptNone, false},
		{"non GPT base", "/v1/responses/compact", "claude-3-5-sonnet", `{"model":"claude-3-5-sonnet-openai-compact","input":"hello"}`, constant.ChannelTypeOpenAI, 204, relaycommon.CompactAttemptBase, false},
		{"non GPT suffix only", "/v1/responses/compact", "claude-3-5-sonnet-openai-compact", `{"model":"claude-3-5-sonnet-openai-compact","input":"hello"}`, constant.ChannelTypeOpenAI, 503, relaycommon.CompactAttemptNone, false},
		{"opaque state rejects conversion", "/v1/responses", "gpt-5", `{"model":"gpt-5","input":[{"type":"compaction_trigger"}]}`, constant.ChannelTypeGemini, 503, relaycommon.CompactAttemptNone, false},
		{"native compaction", "/v1/responses", "gpt-5", `{"model":"gpt-5","context_management":{"compact_threshold":1000},"input":"hello"}`, constant.ChannelTypeOpenAI, 204, relaycommon.CompactAttemptNone, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousDB, previousRedis, previousCache := model.DB, common.RedisEnabled, common.MemoryCacheEnabled
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			model.DB, common.RedisEnabled, common.MemoryCacheEnabled = db, false, false
			t.Cleanup(func() {
				model.DB, common.RedisEnabled, common.MemoryCacheEnabled = previousDB, previousRedis, previousCache
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UserChannelEntitlement{}, &model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}, &model.ChannelHealth{}))
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
				require.Equal(t, test.stage, service.CompactStageFromContext(c))
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
