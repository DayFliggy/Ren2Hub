package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenRouteFirstRetryAndNextRequest(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	createChannelSelectAutoGroupsChannel(t, db, 11, "default", "route-model")
	createChannelSelectAutoGroupsChannel(t, db, 22, "default", "route-model")
	createChannelSelectAutoGroupsChannel(t, db, 33, "vip", "route-model")
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 7, Mode: model.TokenRouteManual, Version: 1}).Error)
	require.NoError(t, db.Create(&[]model.TokenRouteEntry{{TokenID: 7, ChannelID: 33, Position: 0, Enabled: true}, {TokenID: 7, ChannelID: 22, Position: 1, Enabled: true}, {TokenID: 7, ChannelID: 11, Position: 2, Enabled: true}}).Error)
	common.RetryTimes = 2
	for _, cache := range []bool{false, true} {
		common.MemoryCacheEnabled = cache
		if cache {
			model.InitChannelCache()
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		param := &RetryParam{Ctx: c, TokenGroup: "default", ModelName: "route-model"}
		selected, group, err := SelectChannelForRequest(c, "route-model", param)
		require.Nil(t, err)
		require.Equal(t, "default", group)
		require.Equal(t, 22, selected.Id)
		AppendUsedChannel(c, selected.Id)
		param.IncreaseRetry()
		selected, group, selectErr := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, selectErr)
		require.Equal(t, "default", group)
		require.Equal(t, 11, selected.Id)
		AppendUsedChannel(c, selected.Id)
		param.IncreaseRetry()
		selected, _, selectErr = CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, selectErr)
		require.Nil(t, selected)
	}
	// A new WebSocket call gets a new request context and observes the edit.
	require.NoError(t, db.Model(&model.TokenRouteEntry{}).Where("token_id = ? AND channel_id = ?", 7, 22).Update("enabled", false).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenId, 7)
	require.ErrorContains(t, CheckLockedTokenRoute(c, 22), "no longer")
	require.NoError(t, CheckLockedTokenRoute(c, 11))
}

func TestTokenRouteKeepsGroupBoundariesAndTaskContinuations(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	require.NoError(t, db.AutoMigrate(&model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	createChannelSelectAutoGroupsChannel(t, db, 11, "default", "route-model")
	createChannelSelectAutoGroupsChannel(t, db, 22, "vip", "route-model")
	model.InitChannelCache()
	common.RetryTimes = 2
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 7, Mode: model.TokenRouteManual, Version: 1}).Error)
	require.NoError(t, db.Create(&[]model.TokenRouteEntry{{TokenID: 7, ChannelID: 22, Position: 0, Enabled: true}, {TokenID: 7, ChannelID: 11, Position: 1, Enabled: true}}).Error)
	newRequest := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		return c
	}
	for _, cross := range []bool{false, true} {
		c := newRequest()
		common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, cross)
		param := &RetryParam{Ctx: c, TokenGroup: "auto", ModelName: "route-model"}
		channel, group, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.Equal(t, 11, channel.Id)
		require.Equal(t, "default", group, "private order does not reorder upstream pricing groups")
		AppendUsedChannel(c, channel.Id)
		param.IncreaseRetry()
		channel, group, err = CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		if cross {
			require.NotNil(t, channel)
			require.Equal(t, 22, channel.Id)
			require.Equal(t, "vip", group)
		} else {
			require.Nil(t, channel)
		}
	}
	c := newRequest()
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 22, Source: dto.PinSourceToken})
	channel, _, selectionErr := SelectChannelForRequest(c, "route-model", &RetryParam{Ctx: c, TokenGroup: "default"})
	require.Nil(t, channel)
	require.NotNil(t, selectionErr)
	require.Equal(t, http.StatusForbidden, selectionErr.StatusCode)
	c = newRequest()
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"another-model": true})
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 11, Source: dto.PinSourceToken})
	channel, _, selectionErr = SelectChannelForRequest(c, "route-model", &RetryParam{Ctx: c, TokenGroup: "default"})
	require.Nil(t, channel)
	require.NotNil(t, selectionErr)
	require.Equal(t, http.StatusForbidden, selectionErr.StatusCode)
	// A preference edit cannot strand an already authorized origin task.
	require.NoError(t, db.Where("token_id = ?", 7).Delete(&model.TokenRouteEntry{}).Error)
	c = newRequest()
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 22, Source: dto.PinSourceOriginTask})
	channel, _, selectionErr = SelectChannelForRequest(c, "route-model", &RetryParam{Ctx: c, TokenGroup: "default"})
	require.Nil(t, selectionErr)
	require.Equal(t, 22, channel.Id)
}
