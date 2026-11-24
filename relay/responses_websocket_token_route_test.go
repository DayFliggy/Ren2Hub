package relay

import (
	"errors"
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
	"gorm.io/gorm"
)

func TestTokenRouteWebSocketRechecksEachCreate(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.Create(&model.Token{Id: 7, UserId: 17, Key: "ws-routing"}).Error)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	channel := model.Channel{Id: 11, Key: "ws-test", Models: "route-model", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 11, Model: "route-model", Enabled: true}).Error)
	var profile model.TokenRouteProfile
	require.NoError(t, common.Unmarshal([]byte(`{"token_id":7,"mode":"auto","selected_only":true,"version":1}`), &profile))
	require.NoError(t, db.Create(&profile).Error)
	require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: 7, ChannelID: 11, Enabled: true}).Error)
	session := responsesWSSession{lockedChannelID: 11, lockedKey: "ws-test", lockedContext: map[constant.ContextKey]any{constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(), constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride()}}
	newCall := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
		service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: 11, Source: appdto.PinSourceToken})
		return c
	}
	require.Nil(t, session.restoreConnectionContext(newCall(), "route-model"))
	// 全部停用退回全平台后，原渠道仍属于全平台可用渠道，会话可继续。
	require.NoError(t, db.Model(&model.TokenRouteEntry{}).Where("token_id = ? AND channel_id = ?", 7, 11).Update("enabled", false).Error)
	require.Nil(t, session.restoreConnectionContext(newCall(), "route-model"))
	// 若仍有其他启用渠道（22）使范围停留在 selected，则停用的 11 被撤销访问。
	require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: 7, ChannelID: 22, Position: 1, Enabled: true}).Error)
	err := session.restoreConnectionContext(newCall(), "route-model")
	require.NotNil(t, err)
	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.EqualValues(t, "access_denied", err.GetErrorCode())
	require.ErrorContains(t, err, "no longer available")
	require.False(t, service.ShouldRetryRelayError(newCall(), err, 5))
	// 孤儿态（全部条目被删除，selected_only=true 且 entries=0）同样拒绝继续。
	require.NoError(t, db.Where("token_id = ?", 7).Delete(&model.TokenRouteEntry{}).Error)
	err = session.restoreConnectionContext(newCall(), "route-model")
	require.NotNil(t, err)
	require.Equal(t, http.StatusForbidden, err.StatusCode)
	require.EqualValues(t, "access_denied", err.GetErrorCode())
}

func TestTokenRouteWebSocketStorageFailureIsNotRevocation(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.Create(&model.Token{Id: 7, UserId: 17, Key: "ws-routing"}).Error)
	channel := model.Channel{Id: 11, Key: "ws-test", Models: "route-model", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 11, Model: "route-model", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 7, SelectedOnly: true, Mode: model.TokenRouteOrdered, Version: 1}).Error)
	require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: 7, ChannelID: 11, Enabled: true}).Error)
	profileReads := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:ws-route-second-read-error", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "TokenRouteProfile" {
			profileReads++
			if profileReads == 2 {
				tx.AddError(errors.New("private database routing fixture"))
			}
		}
	}))
	session := responsesWSSession{lockedChannelID: 11, lockedKey: "ws-test", lockedContext: map[constant.ContextKey]any{constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(), constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride()}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenId, 7)
	service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: 11, Source: appdto.PinSourceToken})
	before := service.ChannelInFlight(11)
	err := session.restoreConnectionContext(c, "route-model")
	require.NotNil(t, err)
	require.Equal(t, 2, profileReads, "the storage error must occur in the final acquisition, after the first authorization check")
	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
	require.EqualValues(t, "get_channel_failed", err.GetErrorCode())
	require.NotContains(t, err.Error(), "private database")
	require.False(t, service.ShouldRetryRelayError(c, err, 5))
	require.Equal(t, before, service.ChannelInFlight(11))
	require.False(t, c.Writer.Written())
}

func TestTokenRouteWebSocketPinnedAbilityRevocation(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.Create(&model.Token{Id: 7, UserId: 17, Key: "ws-routing"}).Error)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	channel := model.Channel{Id: 11, Key: "ws-test", Models: "route-model", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	channel.SetSetting(dto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 11, Model: "route-model", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 7, SelectedOnly: true, Mode: model.TokenRouteOrdered, Version: 1}).Error)
	require.NoError(t, db.Create(&model.TokenRouteEntry{TokenID: 7, ChannelID: 11, Enabled: true}).Error)
	session := responsesWSSession{lockedChannelID: 11, lockedKey: "ws-test", lockedContext: map[constant.ContextKey]any{constant.ContextKeyChannelBaseUrl: channel.GetBaseURL(), constant.ContextKeyChannelHeaderOverride: channel.GetHeaderOverride()}}
	newCall := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
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
