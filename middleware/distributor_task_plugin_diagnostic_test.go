package middleware

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPluginNoChannelDiagnosticDoesNotExposePluginKeys(t *testing.T) {
	require.NoError(t, i18n.Init())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousRedis, previousCache, previousRDB := model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.RDB
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.RDB = db, true, false, client
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.MemoryCacheEnabled, common.RDB = previousDB, previousRedis, previousCache, previousRDB
		require.NoError(t, client.Close())
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserRouteProfile{}, &model.Channel{}, &model.Ability{}, &model.UserChannelEntitlement{}, &model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}, &model.ChannelHealth{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "fixture", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))

	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})
	for _, test := range []struct {
		name, language, want string
		candidates           bool
	}{
		{"native English", "en", "requires an enabled, eligible plugin channel", false},
		{"shared model simplified Chinese", "zh-CN", "需要已启用且符合条件的插件渠道", true},
		{"shared model traditional Chinese", "zh-TW", "需要已啟用且符合條件的外掛管道", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/fixture/submit", nil)
			c.Request.Header.Set("Accept-Language", test.language)
			c.Set(common.RequestIdKey, "plugin-diagnostic-request")
			common.SetContextKey(c, constant.ContextKeyUserId, 1)
			common.SetContextKey(c, constant.ContextKeyTokenId, 2)
			if test.candidates {
				c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{Candidates: []jsplugin.ProtocolBinding{
					{Plugin: &jsplugin.LoadedPlugin{Meta: jsplugin.Meta{Key: "private-alpha"}}},
					{Plugin: &jsplugin.LoadedPlugin{Meta: jsplugin.Meta{Key: "private-beta"}}},
				}})
			} else {
				c.Set("expected_task_plugin_key", "private-alpha")
			}
			channel, apiErr := SelectRequestChannel(c, "task-model", 0)
			require.Nil(t, channel)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
			abortWithOpenAiMessage(c, apiErr.StatusCode, apiErr.Error(), apiErr.GetErrorCode())
			require.Contains(t, w.Body.String(), test.want)
			require.Contains(t, w.Body.String(), "task-model")
			require.NotContains(t, w.Body.String(), "private-alpha")
			require.NotContains(t, w.Body.String(), "private-beta")
		})
	}
	require.Contains(t, logs.String(), "plugin-diagnostic-request")
	require.Contains(t, logs.String(), "private-alpha")
	require.Contains(t, logs.String(), "private-beta")
}
