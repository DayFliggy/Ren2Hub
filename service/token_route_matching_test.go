package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenRouteCandidatesConcreteModelNormalization(t *testing.T) {
	db := setupRouteTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	user := &model.User{Username: "routing-model-owner", AffCode: "routing-model"}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "routing-model-token", ModelLimitsEnabled: true, ModelLimits: "gpt-4-gizmo-audit"}
	require.NoError(t, db.Create(token).Error)
	createRouteChannel(t, db, 11, "gpt-4-gizmo-*")
	createRouteChannel(t, db, 22, "gpt-4-gizmo-other")
	createRouteChannel(t, db, 33, "gpt-4-gizmo-*")
	createRouteChannel(t, db, 44, "gpt-4-gizmo-*")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 44).Update("status", common.ChannelStatusManuallyDisabled).Error)
	channels, err := TokenRouteChannels(context.Background(), user.Id, token.Id)
	require.NoError(t, err)
	require.Len(t, channels, 3, "concrete sibling is excluded; disabled channel is visible with its reason")
	require.False(t, channels[2].Eligible)
	require.Equal(t, 11, channels[0].ID)
	require.Equal(t, []string{"gpt-4-gizmo-audit"}, channels[0].Models, "the editor must not imply wildcard authorization")
	view, err := PutTokenRouting(context.Background(), user.Id, token.Id, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual}, Entries: []model.TokenRouteEntry{{ChannelID: 11, Enabled: true}}})
	require.NoError(t, err)
	require.EqualValues(t, 1, view.Version)
	preview, err := PreviewTokenRouting(context.Background(), user.Id, token.Id, TokenRoutePreviewQuery{Model: "gpt-4-gizmo-audit", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	require.Len(t, preview.Entries, 1)
	require.True(t, preview.Entries[0].Eligible)
	for _, cache := range []bool{false, true} {
		common.MemoryCacheEnabled = cache
		if cache {
			model.InitChannelCache()
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
		selected, apiErr := SelectChannelForRequest(c, "gpt-4-gizmo-audit", &RetryParam{Ctx: c, ModelName: "gpt-4-gizmo-audit"})
		require.Nil(t, apiErr)
		require.Equal(t, 11, selected.Id)
	}
	_, err = PutTokenRouting(context.Background(), user.Id, token.Id, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual, Version: 1}, Entries: []model.TokenRouteEntry{{ChannelID: 22, Enabled: true}}})
	require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
}

func TestTokenRouteCandidateModelWitnesses(t *testing.T) {
	for _, tc := range []struct {
		name, limit, ability   string
		unrestricted, thinking bool
		want                   []string
	}{
		{name: "exact", limit: "gpt-4o", ability: "gpt-4o", want: []string{"gpt-4o"}},
		{name: "thinking parameters", limit: "qwen3-max@thinking:on@temperature:0.2", ability: "qwen3-max", want: []string{"qwen3-max@thinking:on@temperature:0.2"}},
		{name: "claude suffix", limit: "claude-3-7-sonnet-thinking", ability: "claude-3-7-sonnet", want: []string{"claude-3-7-sonnet-thinking"}},
		{name: "gemini wildcard", limit: "gemini-2.5-flash-thinking-8192", ability: "gemini-2.5-flash-thinking-*", want: []string{"gemini-2.5-flash-thinking-8192"}},
		{name: "gemini adapter", limit: "gemini-2.5-flash-thinking-8192", ability: "gemini-2.5-flash", thinking: true, want: []string{"gemini-2.5-flash-thinking-8192"}},
		{name: "declared wildcard", limit: "gpt-4-gizmo-*", ability: "gpt-4-gizmo-concrete", want: []string{"gpt-4-gizmo-concrete"}},
		{name: "different concrete siblings", limit: "gpt-4-gizmo-a", ability: "gpt-4-gizmo-b"},
		{name: "no invented glob", limit: "unknown-*", ability: "unknown-concrete"},
		{name: "empty restriction", ability: "gpt-4o"},
		{name: "unrestricted", ability: "gpt-4-gizmo-*", unrestricted: true, want: []string{"gpt-4-gizmo-*"}},
		{name: "opaque exempt", limit: "opaque@sha256:deadbeef", ability: "opaque@sha256:deadbeef", want: []string{"opaque@sha256:deadbeef"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRouteTest(t)
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
			gemini := model_setting.GetGeminiSettings()
			previousThinking := gemini.ThinkingAdapterEnabled
			gemini.ThinkingAdapterEnabled = tc.thinking
			settings := model_setting.GetGlobalSettings()
			previousBlacklist := append([]string(nil), settings.ThinkingModelBlacklist...)
			settings.ThinkingModelBlacklist = append(settings.ThinkingModelBlacklist, "re:.*@sha256:.*")
			t.Cleanup(func() {
				gemini.ThinkingAdapterEnabled = previousThinking
				settings.ThinkingModelBlacklist = previousBlacklist
			})
			user := &model.User{Username: "witness", AffCode: "witness"}
			require.NoError(t, db.Create(user).Error)
			token := &model.Token{UserId: user.Id, Key: "witness-token", ModelLimitsEnabled: !tc.unrestricted, ModelLimits: tc.limit}
			require.NoError(t, db.Create(token).Error)
			createRouteChannel(t, db, 11, tc.ability)
			channels, err := TokenRouteChannels(context.Background(), user.Id, token.Id)
			require.NoError(t, err)
			if tc.want == nil {
				require.Empty(t, channels)
				return
			}
			require.Len(t, channels, 1)
			require.Equal(t, tc.want, channels[0].Models)
			for _, name := range channels[0].Models {
				require.True(t, tc.unrestricted || TokenModelLimitAllows(token.GetModelLimitsMap(), name))
				common.MemoryCacheEnabled = false
				enabled, err := model.CheckChannelEnabledForModel(name, channels[0].ID)
				require.NoError(t, err)
				require.True(t, enabled, "each published model must be an actual request witness")
			}
		})
	}
}
