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

func TestTokenRoutingPreviewIsOwnedReadOnlyAndExplainsConstraints(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.User{Id: 71, Username: "preview-owner"}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 72, UserId: 71, Key: "preview-token", RemainQuota: 12345, ModelLimitsEnabled: true, ModelLimits: "fixture"}).Error)
	for _, ch := range []model.Channel{
		{Id: 1, Name: "Eligible", Type: constant.ChannelTypeOpenAI, Status: 1, Models: "fixture", Key: "not-public"},
		{Id: 2, Name: "Advanced", Type: constant.ChannelTypeAdvancedCustom, Status: 1, Models: "fixture"},
		{Id: 3, Name: "Another model", Type: constant.ChannelTypeOpenAI, Status: 1, Models: "other"},
		{Id: 4, Name: "Over ratio limit", Type: constant.ChannelTypeOpenAI, Status: 1, ChannelRatio: "2", Models: "fixture"},
		{Id: 5, Name: "Disabled", Type: constant.ChannelTypeOpenAI, Status: 2, Models: "fixture"},
	} {
		require.NoError(t, db.Create(&ch).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: ch.Id, Model: ch.Models, Enabled: ch.Status == 1}).Error)
	}
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 72, Mode: "ordered", SelectedOnly: true, Version: 3, MaxChannelRatio: common.GetPointer("1")}).Error)
	entries := []model.TokenRouteEntry{}
	for i, id := range []int{2, 4, 5, 3, 1} {
		entries = append(entries, model.TokenRouteEntry{TokenID: 72, ChannelID: id, Position: i, Enabled: true})
	}
	require.NoError(t, db.Create(&entries).Error)
	request := func(user int, query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/token/72/routing/preview?"+query, nil)
		c.Params = gin.Params{{Key: "id", Value: "72"}}
		c.Set("id", user)
		GetTokenRoutingPreview(c)
		return w
	}
	w := request(71, "model=fixture&path=/v1/responses")
	require.Equal(t, 200, w.Code)
	var result struct {
		Data struct {
			Mode    string `json:"mode"`
			Version int    `json:"version"`
			Entries []struct {
				ChannelID int    `json:"channel_id"`
				Eligible  bool   `json:"eligible"`
				Reason    string `json:"reason"`
			} `json:"entries"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "ordered", result.Data.Mode)
	require.Equal(t, 3, result.Data.Version)
	require.Len(t, result.Data.Entries, 5)
	require.Equal(t, "protocol_unavailable", result.Data.Entries[0].Reason)
	require.Equal(t, "channel_ratio_exceeded", result.Data.Entries[1].Reason)
	require.Equal(t, "channel_unavailable", result.Data.Entries[2].Reason)
	require.Equal(t, "model_unavailable", result.Data.Entries[3].Reason)
	require.True(t, result.Data.Entries[4].Eligible)
	require.NotContains(t, w.Body.String(), "not-public")
	require.NotContains(t, w.Body.String(), "not-public")
	require.Equal(t, 404, request(99, "model=fixture&path=/v1/responses").Code)
	require.Equal(t, 400, request(71, "model=fixture&path=https://invalid.example").Code)
	require.Equal(t, 400, request(71, "model=fixture&path=/v1/responses&stream=maybe").Code)
	require.Contains(t, request(71, "model=fixture&path=/v1/responses&stream=true").Body.String(), `"stream":true`)
	require.Contains(t, w.Body.String(), `"ordering":"ordered"`)
	require.Contains(t, w.Body.String(), `"transport":"http"`)
	ws := request(71, "model=fixture&path=/v1/responses&transport=websocket")
	require.Equal(t, 200, ws.Code)
	require.Contains(t, ws.Body.String(), `"transport":"websocket"`)
	require.Contains(t, ws.Body.String(), `"stream":true`)
	require.NotContains(t, ws.Body.String(), `"eligible":true`)
	for _, suffix := range []string{"&path=/v1/chat/completions&transport=websocket", "&path=/custom/plugin"} {
		unsupported := request(71, "model=fixture"+suffix)
		require.Equal(t, 400, unsupported.Code)
		require.Contains(t, unsupported.Body.String(), "preview_path_unsupported")
	}
	require.Equal(t, 400, request(71, "model=fixture&path=/v1/responses&transport=invalid").Code)

	require.Contains(t, request(71, "model=forbidden&path=/v1/responses").Body.String(), "model_not_allowed")
	token, err := model.GetTokenByIds(72, 71)
	require.NoError(t, err)
	require.Equal(t, 12345, token.RemainQuota)
	view, err := model.ReadTokenRouting(t.Context(), 72)
	require.NoError(t, err)
	require.EqualValues(t, 3, view.Version)
	require.Len(t, view.Entries, 5)
	require.NoError(t, db.Where("token_id = ?", 72).Delete(&model.TokenRouteEntry{}).Error)
	require.Contains(t, request(71, "model=fixture&path=/v1/responses").Body.String(), `"entries":[]`)
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
