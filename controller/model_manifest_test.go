package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelManifestPublicProjectionAndRetrieveSharePermissions(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withSelfUseModeEnabled(t)
	const allowed = "custom-manifest-model"
	const forbidden = "gpt-5.4"
	require.NoError(t, db.Create(&model.Channel{Id: 801, Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: allowed + "," + forbidden, Key: "must-stay-private"}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 801, Model: allowed, Enabled: true}, {ChannelId: 801, Model: forbidden, Enabled: true},
	}).Error)
	toolCall, source := false, "internal-administrator-source"
	card := model.Model{ModelName: allowed, Status: 1, Capabilities: &model.ModelCapabilities{ToolCall: &toolCall, Source: &source}}
	require.NoError(t, card.Insert())
	refreshModelListCapabilities(t, db)

	request := func(name string) (*gin.Context, *httptest.ResponseRecorder) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models/"+name, nil)
		ctx.Params = gin.Params{{Key: "model", Value: name}}
		common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{allowed: true})
		return ctx, recorder
	}
	ctx, recorder := request("")
	ListModels(ctx, constant.ChannelTypeOpenAI)
	payload := decodeListModelsPayload(t, recorder)
	require.Len(t, payload.Data, 1)
	assert.Equal(t, allowed, payload.Data[0].Id)
	for _, privateField := range []string{"capabilities", "internal-administrator-source", "price_version", "price_configured", "must-stay-private"} {
		assert.NotContains(t, recorder.Body.String(), privateField)
	}
	ctx, recorder = request(allowed)
	RetrieveModel(ctx, constant.ChannelTypeOpenAI)
	var item map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &item))
	assert.Equal(t, allowed, item["id"], "custom models are retrievable through the same manifest")
	assert.NotContains(t, item, "capabilities")
	ctx, recorder = request(forbidden)
	RetrieveModel(ctx, constant.ChannelTypeOpenAI)
	var denied struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &denied))
	assert.Equal(t, "model_not_found", denied.Error.Code, "the static provider catalog cannot bypass model permission filtering")
}
