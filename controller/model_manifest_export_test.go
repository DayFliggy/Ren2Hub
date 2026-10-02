package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserManifestAndClientConfigUseOwnedTokenAndTrustedServerAddress(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withSelfUseModeEnabled(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	require.NoError(t, db.Create(&model.Token{Id: 77, UserId: 7, Key: "never-export-me", Status: common.TokenStatusEnabled, ExpiredTime: -1, ModelLimitsEnabled: true, ModelLimits: "allowed"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 811, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "allowed,forbidden"}).Error)
	require.NoError(t, db.Create(&[]model.Ability{{ChannelId: 811, Model: "allowed", Enabled: true}, {ChannelId: 811, Model: "forbidden", Enabled: true}}).Error)
	refreshModelListCapabilities(t, db)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://trusted.example.test"
	t.Cleanup(func() { system_setting.ServerAddress = previous })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 7) })
	router.GET("/manifest", GetUserModelManifest)
	router.GET("/token/:id/client-config", GetTokenClientConfig)
	for _, path := range []string{"/manifest?token_id=77", "/token/77/client-config?format=pi"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "https://attacker.example.test"+path, nil)
		request.Header.Set("X-Forwarded-Host", "attacker.example.test")
		router.ServeHTTP(response, request)
		assert.Contains(t, response.Body.String(), "allowed")
		for _, hidden := range []string{"forbidden", "never-export-me", "attacker.example.test"} {
			assert.NotContains(t, response.Body.String(), hidden)
		}
		if path != "/manifest?token_id=77" {
			assert.Contains(t, response.Body.String(), "trusted.example.test/v1")
		}
	}
	for _, path := range []string{"/manifest?token_id=-1", "/manifest?token_id=99", "/token/99/client-config?format=pi", "/token/77/client-config?format=pi&model=forbidden"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Contains(t, response.Body.String(), `"success":false`)
		assert.NotContains(t, response.Body.String(), "never-export-me")
	}
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 77).Update("user_id", 8).Error)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/manifest?token_id=77", nil))
	assert.Contains(t, response.Body.String(), `"success":false`)
	assert.NotContains(t, response.Body.String(), "allowed")
}
