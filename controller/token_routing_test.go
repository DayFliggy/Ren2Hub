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
	"github.com/QuantumNous/new-api/service/authz"
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
		{"manual", `"0.125"`, common.GetPointer("0.125")},
		{"manual", `null`, nil},
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
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"manual","version":0,"entries":[{"channel_id":22,"enabled":true}]}`, PutTokenRouting)
	require.Equal(t, 403, w.Code)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"manual","version":0,"entries":[{"channel_id":11,"enabled":true}]}`, PutTokenRouting)
	require.Equal(t, 200, w.Code)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"auto","version":0,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "ROUTING_VERSION_CONFLICT")
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"manual","version":1,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"mode":"manual"`)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"auto","version":2,"entries":[]}`, PutTokenRouting)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"version":3`)
	w = request(17, http.MethodPut, "/api/token/27/routing", `{"mode":"manual","entries":[]}`, PutTokenRouting)
	require.Equal(t, 400, w.Code)
}

func TestTokenRoutingCreationHonorsWritePermission(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	require.NoError(t, authz.Init(db))
	create := func(name string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/token/", strings.NewReader(`{"name":"`+name+`","type":"manual","unlimited_quota":true,"expired_time":-1}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", 17)
		c.Set("role", common.RoleCommonUser)
		AddToken(c)
		return w
	}
	w := create("manual")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	var token model.Token
	require.NoError(t, db.Where("user_id = ?", 17).First(&token).Error)
	view, err := model.ReadTokenRouting(t.Context(), token.Id)
	require.NoError(t, err)
	require.Equal(t, model.TokenRouteManual, view.Mode)
	require.Empty(t, view.Entries)
	responses := buildMaskedTokenResponses([]*model.Token{&token})
	require.NoError(t, fillTokenRoutingModes(responses))
	require.Equal(t, model.TokenRouteManual, responses[0].Type)
	require.NoError(t, authz.SetUserPermissions(17, authz.PermissionsMap{authz.ResourceRouting: {authz.ActionWrite: false}}))
	w = create("forbidden")
	require.Equal(t, http.StatusForbidden, w.Code)
	var count int64
	require.NoError(t, db.Model(&model.Token{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
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
	require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 72, Mode: "manual", Version: 3, MaxChannelRatio: common.GetPointer("1")}).Error)
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
	require.Equal(t, "manual", result.Data.Mode)
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
	require.Contains(t, w.Body.String(), `"ordering":"manual"`)
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
