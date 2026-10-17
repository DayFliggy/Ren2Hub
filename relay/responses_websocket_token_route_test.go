package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenRouteWebSocketRechecksEachCreate(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	channel := model.Channel{Id: 11, Key: "ws-test", Group: "default", Models: "route-model", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 11, Group: "default", Model: "route-model", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 7, Mode: model.TokenRouteManual, Version: 1}).Error)
	require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: 7, ChannelID: 11, Enabled: true}).Error)
	session := responsesWSSession{lockedChannelID: 11, lockedKey: "ws-test", lockedContext: map[constant.ContextKey]any{constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(), constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride()}}
	newCall := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: 11, Source: appdto.PinSourceToken})
		return c
	}
	require.Nil(t, session.restoreConnectionContext(newCall(), "route-model"))
	require.NoError(t, db.Model(&model.TokenRouteEntry{}).Where("token_id = ?", 7).Update("enabled", false).Error)
	err := session.restoreConnectionContext(newCall(), "route-model")
	require.NotNil(t, err)
	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.ErrorContains(t, err, "no longer in the token's private route")
	require.False(t, service.ShouldRetryRelayError(newCall(), err, 5))
}

func TestTokenRouteWebSocketPinnedAbilityRevocation(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	channel := model.Channel{Id: 11, Key: "ws-test", Group: "default", Models: "route-model", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 11, Group: "default", Model: "route-model", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 7, Mode: model.TokenRouteManual, Version: 1}).Error)
	require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: 7, ChannelID: 11, Enabled: true}).Error)
	session := responsesWSSession{lockedChannelID: 11, lockedGroup: "default", lockedKey: "ws-test", lockedContext: map[constant.ContextKey]any{constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(), constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride()}}
	newCall := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: 11, Source: appdto.PinSourceToken})
		return c
	}
	require.Nil(t, session.restoreConnectionContext(newCall(), "route-model"))
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 11).Update("enabled", false).Error)
	err := session.restoreConnectionContext(newCall(), "route-model")
	require.NotNil(t, err, "a manual route plus administrator pin must recheck current model/group eligibility on every create")
	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.False(t, service.ShouldRetryRelayError(newCall(), err, 5))
}
