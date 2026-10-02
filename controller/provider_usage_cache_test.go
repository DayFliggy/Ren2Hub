package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProviderUsageControllerCacheAndSettings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.GatewayCredential{}, &model.ProviderUsageSnapshot{}, &model.ProviderUsageSetting{}))
	channel := model.Channel{Id: 75, Type: constant.ChannelTypeCodex, Key: `{"access_token":"fixture-secret","account_id":"fixture-account"}`, BaseURL: common.GetPointer("http://127.0.0.1:1")}
	require.NoError(t, db.Create(&channel).Error)
	engine := gin.New()
	engine.GET("/api/channel/:id/provider-usage", GetChannelProviderUsage)
	engine.GET("/api/channel/:id/codex/usage", GetCodexChannelUsage)
	engine.PUT("/api/channel/:id/provider-usage/settings", UpdateChannelProviderUsageSettings)
	for _, path := range []string{"/api/channel/75/provider-usage", "/api/channel/75/codex/usage"} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), `"state":"unknown"`)
		assert.Contains(t, response.Body.String(), `"auto_refresh":false`)
		assert.NotContains(t, response.Body.String(), "fixture-secret")
		assert.NotContains(t, response.Body.String(), "fixture-account")
	}
	var identities int64
	require.NoError(t, db.Model(&model.GatewayCredential{}).Count(&identities).Error)
	assert.Zero(t, identities, "GET must not create a credential row")
	for _, body := range []string{`{}`, `{"auto_refresh":null}`, `{"auto_refresh":"true"}`} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/channel/75/provider-usage/settings", strings.NewReader(body)))
		assert.Equal(t, http.StatusBadRequest, response.Code)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/channel/75/provider-usage/settings", strings.NewReader(`{"auto_refresh":true}`)))
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"auto_refresh":true`)
	require.NoError(t, db.Migrator().DropTable(&model.ProviderUsageSnapshot{}))
	_, err = model.EnsureGatewayCredential(t.Context(), channel.Id, channel.Key)
	require.NoError(t, err)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/channel/75/provider-usage", nil))
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Contains(t, response.Body.String(), `"success":false`)
}
