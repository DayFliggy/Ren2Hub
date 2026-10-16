package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMarketRouteRequiresLoginAndRefreshesUserSupply(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{}))
	memoryEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = memoryEnabled })
	const userID = 987
	require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "market-viewer", AffCode: "market-viewer",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	session := createRouterBrowserSession(t, userID)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 988, Name: "Visible supply", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: "market-route-model", ChannelRatio: "0.25", Key: "private-market-upstream-key"}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{ChannelId: 988, Model: "market-route-model", Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.Option{Key: "ModelPrice", Value: `{"market-route-model":"0.01"}`}).Error)
	engine := gin.New()
	SetApiRouter(engine)
	call := func(token string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/user/model-market", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		engine.ServeHTTP(response, request)
		return response
	}
	assert.Equal(t, http.StatusUnauthorized, call("").Code)
	assert.Equal(t, http.StatusUnauthorized, call("invalid-session").Code)
	response := call(session)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	var payload struct {
		Success bool            `json:"success"`
		Data    dto.ModelMarket `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success, response.Body.String())
	require.Len(t, payload.Data.Models, 1)
	require.Len(t, payload.Data.Channels, 1)
	require.Len(t, payload.Data.Offers, 3)
	assert.Equal(t, "0.25", payload.Data.Channels[0].Ratio)
	assert.Equal(t, "0.01", payload.Data.Models[0].Baselines[0].Values["ModelPrice"])
	assert.NotContains(t, response.Body.String(), "private-market-upstream-key")

	require.NoError(t, model.DB.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 988,
		Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
	response = call(session)
	require.Equal(t, http.StatusOK, response.Code)
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Empty(t, payload.Data.Models)
	assert.Empty(t, payload.Data.Channels)
	assert.Empty(t, payload.Data.Offers)
	assert.Contains(t, response.Body.String(), `"models":[]`, "an empty authorized market is a successful, non-null collection")

	// Storage failures are translated errors, not empty successful catalogs.
	require.NoError(t, model.DB.Migrator().DropTable(&model.Ability{}))
	response = call(session)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Contains(t, response.Body.String(), `"message_key":"Model market is unavailable"`)
	assert.NotContains(t, response.Body.String(), "abilities")
}
