package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenRoutingAPIPersistsAndClearsChannelRatioCap(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "ratio-cap-owner"}).Error)
	require.NoError(t, (&model.Token{Id: 27, UserId: 17, Key: "ratio-cap-token"}).Insert())
	request := func(method, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "/api/token/27/routing", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", 17)
		c.Params = gin.Params{{Key: "id", Value: "27"}}
		handler(c)
		return w
	}
	for i, tc := range []struct {
		mode, input string
		want        *string
	}{
		{"auto", `"1.500"`, common.GetPointer("1.5")},
		{"auto", `"0.125"`, common.GetPointer("0.125")},
		{"auto", `null`, nil},
	} {
		body := fmt.Sprintf(`{"mode":%q,"version":%d,"entries":[],"max_channel_ratio":%s}`, tc.mode, i, tc.input)
		w := request(http.MethodPut, body, PutTokenRouting)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		for _, response := range []*httptest.ResponseRecorder{w, request(http.MethodGet, "", GetTokenRouting)} {
			var result struct {
				Success bool               `json:"success"`
				Data    model.TokenRouting `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success)
			require.Equal(t, tc.mode, result.Data.Mode)
			require.EqualValues(t, i+1, result.Data.Version)
			require.Equal(t, tc.want, result.Data.MaxChannelRatio)
		}
		stored, err := model.ReadTokenRouting(t.Context(), 27)
		require.NoError(t, err)
		require.Equal(t, tc.want, stored.MaxChannelRatio)
	}
	w := request(http.MethodPut, `{"mode":"auto","version":3,"entries":[],"max_channel_ratio":"-1"}`, PutTokenRouting)
	require.Equal(t, http.StatusBadRequest, w.Code)
	stored, err := model.ReadTokenRouting(t.Context(), 27)
	require.NoError(t, err)
	require.EqualValues(t, 3, stored.Version)
	require.Nil(t, stored.MaxChannelRatio)
}

func TestTokenRoutingAPIIsolationConflictAndPublicProjection(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "routing-owner"}).Error)
	token := model.Token{Id: 27, UserId: 17, Key: "token-api-fixture", ModelLimitsEnabled: true, ModelLimits: "visible-model"}
	require.NoError(t, token.Insert())
	url := "https://upstream-private.example"
	for _, channel := range []model.Channel{
		{Id: 11, Name: "Public label", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "upstream-secret", BaseURL: &url, Models: "visible-model,hidden-model"},
		{Id: 22, Name: "Disabled route", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled, Models: "visible-model"},
	} {
		require.NoError(t, db.Create(&channel).Error)
		for _, name := range channel.GetModels() {
			require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: name, Enabled: true}).Error)
		}
	}
	request := func(user int, method, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", user)
		c.Params = gin.Params{{Key: "id", Value: "27"}}
		handler(c)
		return w
	}
	w := request(18, http.MethodGet, "/api/token/27/routing", "", GetTokenRouting)
	require.Equal(t, 404, w.Code)
	w = request(17, http.MethodGet, "/api/token/27/routing/channels", "", GetTokenRoutingChannels)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "Public label")
	require.NotContains(t, w.Body.String(), "upstream-secret")
	require.NotContains(t, w.Body.String(), "upstream-private")
	require.NotContains(t, w.Body.String(), "hidden-model")
	require.Contains(t, w.Body.String(), "channel_unavailable")
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"ordered","selected_only":true,"version":0,"entries":[{"channel_id":22,"enabled":true}]}`, PutTokenRouting)
	require.Equal(t, 403, w.Code)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"ordered","selected_only":true,"version":0,"entries":[{"channel_id":11,"enabled":true}]}`, PutTokenRouting)
	require.Equal(t, 200, w.Code)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"auto","version":0,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "ROUTING_VERSION_CONFLICT")
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"ordered","selected_only":true,"version":1,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 400, w.Code)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"auto","version":1,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"version":2`)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"ordered","selected_only":true,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 400, w.Code)
}

func TestTokenRoutingPUTRestoresPlatformSelection(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "scope-reset"}).Error)
	token := model.Token{Id: 27, UserId: 17, Key: "scope-reset"}
	require.NoError(t, token.Insert())
	for _, id := range []int{11, 22} {
		channel := model.Channel{Id: id, Key: "scope-test", Type: constant.ChannelTypeOpenAI, Models: "scope-model", Status: common.ChannelStatusEnabled}
		require.NoError(t, channel.Insert())
	}
	put := func(body string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/token/27/routing", strings.NewReader(body))
		c.Set("id", 17)
		c.Params = gin.Params{{Key: "id", Value: "27"}}
		PutTokenRouting(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	put(`{"mode":"auto","selected_only":true,"version":0,"entries":[{"channel_id":11,"enabled":true}]}`)
	put(`{"mode":"auto","selected_only":false,"version":1,"entries":[]}`)
	saved, err := model.ReadTokenRouting(t.Context(), 27)
	require.NoError(t, err)
	require.False(t, saved.SelectedOnly)
	require.EqualValues(t, 2, saved.Version)
	require.Empty(t, saved.Entries)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyTokenId, 27)
	defer service.ReleaseRouteChannel(c)
	channel, failure := service.SelectChannelForRequest(c, "scope-model", nil)
	require.Nil(t, failure)
	require.Equal(t, 22, channel.Id)
}

func TestAddTokenReturnsCreatedID(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{Id: 31, Username: "token-creator"}).Error)
	ctx, w := newAuthenticatedContext(t, http.MethodPost, "/api/token/", map[string]any{"name": "new-key", "unlimited_quota": true}, 31)
	AddToken(ctx)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Greater(t, payload.Data.ID, 0)
	stored, err := model.GetTokenByIds(payload.Data.ID, 31)
	require.NoError(t, err)
	require.Equal(t, "new-key", stored.Name)
}

func TestGetTokenRoutingCandidateChannelsFiltersDraftPolicy(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.User{Id: 35, Username: "candidate-owner"}).Error)
	privateURL := "https://private-candidate.example"
	for _, ch := range []model.Channel{
		{Id: 11, Name: "Cheap-A", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "secret-a", BaseURL: &privateURL, Models: "gpt-4o,claude-4", ChannelRatio: "1"},
		{Id: 22, Name: "Pricey-B", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "secret-b", Models: "gpt-4o", ChannelRatio: "2.5"},
		{Id: 33, Name: "Other-C", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "secret-c", Models: "other-model", ChannelRatio: "1"},
		{Id: 44, Name: "Disabled-D", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled, Key: "secret-d", Models: "gpt-4o", ChannelRatio: "1"},
	} {
		require.NoError(t, db.Create(&ch).Error)
		for _, m := range ch.GetModels() {
			require.NoError(t, db.Create(&model.Ability{ChannelId: ch.Id, Model: m, Enabled: true}).Error)
		}
	}
	query := func(rawQuery string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/token/routing/channels"+rawQuery, nil)
		c.Set("id", 35)
		GetTokenRoutingCandidateChannels(c)
		return w
	}

	w := query("?model_limits=gpt-4o&max_channel_ratio=1.5")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "secret-a")
	require.NotContains(t, w.Body.String(), "private-candidate")
	var resp struct {
		Success bool                   `json:"success"`
		Data    []service.RouteChannel `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	byID := map[int]service.RouteChannel{}
	for _, ch := range resp.Data {
		byID[ch.ID] = ch
	}
	require.NotContains(t, byID, 33, "channel serving no allowed model is excluded")
	require.True(t, byID[11].Eligible)
	require.False(t, byID[22].Eligible)
	require.Equal(t, "channel_ratio_exceeded", byID[22].FilterReason)
	require.False(t, byID[44].Eligible)
	require.Equal(t, "channel_unavailable", byID[44].FilterReason)

	require.Equal(t, http.StatusBadRequest, query("?max_channel_ratio=-1").Code)
	require.Equal(t, http.StatusBadRequest, query("?model_limits="+strings.Repeat("a", maxCandidateModelLimitsBytes+1)).Code)
}
