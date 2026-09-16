package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyOperationPlansUseExecutablePaths(t *testing.T) {
	for _, path := range []string{"/v1/completions", "/v1/moderations"} {
		for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeAzure, constant.ChannelTypeNewAPI, constant.ChannelTypeAnthropic, constant.ChannelTypeGemini, constant.ChannelTypeOllama, constant.ChannelTypeVertexAi, constant.ChannelTypeSub2API, constant.ChannelTypeDeepSeek, constant.ChannelTypeAli, constant.ChannelTypeMoonshot} {
			requirements, err := analyzeRoutePreviewRequest(RouteProfilePreviewInput{Model: "test", Path: path})
			require.NoError(t, err)
			assert.NotEqual(t, "text", requirements.Operation)
			allowed := channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeAzure || channelType == constant.ChannelTypeNewAPI ||
				(path == "/v1/completions" && (channelType == constant.ChannelTypeDeepSeek || channelType == constant.ChannelTypeAli || channelType == constant.ChannelTypeMoonshot))
			for _, policy := range []types.ConversionLossPolicy{"safe", "strict", "allow"} {
				plan, err := PlanChannelConversion(requirements, &model.Channel{Type: channelType}, path, "test", policy)
				if allowed {
					require.NoError(t, err, "path=%s type=%d policy=%s", path, channelType, policy)
					assert.True(t, plan.Native)
					assert.Equal(t, requirements.Operation, plan.Operation)
				} else {
					require.ErrorContains(t, err, "request path", "path=%s type=%d policy=%s", path, channelType, policy)
				}
			}
		}
	}
}

func TestLegacyOperationsIgnoreChatResponsesUpgrade(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	old := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = old })
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{".*"}}
	for _, path := range []string{"/v1/completions", "/v1/moderations"} {
		req, err := analyzeRoutePreviewRequest(RouteProfilePreviewInput{Model: "test", Path: path})
		require.NoError(t, err)
		plan, err := PlanChannelConversion(req, &model.Channel{Type: constant.ChannelTypeOpenAI}, path, "test", "safe")
		require.NoError(t, err)
		assert.True(t, plan.Native)
		assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAI), plan.To)
	}
}

func TestLegacyOperationPreviewFiltersBeforePlanning(t *testing.T) {
	for _, path := range []string{"/v1/completions", "/v1/moderations"} {
		for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeAnthropic} {
			t.Run(fmt.Sprintf("%s/%d", path, channelType), func(t *testing.T) {
				db := setupRouteProfileTest(t)
				userID, tokenID, channelID := seedRouteProfileFixture(t, db)
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("type", channelType).Error)
				publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
				profile, err := CreateUserRouteProfile(RouteProfileInput{
					UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
					Groups: []RouteGroupInput{{Name: "primary", Enabled: true, Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}}}},
				})
				require.NoError(t, err)
				preview, err := PreviewUserRouteProfile(context.Background(), userID, profile.Profile.ID, RouteProfilePreviewInput{Model: "gpt-test", Path: path})
				require.NoError(t, err)
				require.Len(t, preview.Entries, 1)
				if channelType == constant.ChannelTypeOpenAI {
					assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
					require.NotNil(t, preview.Entries[0].ConversionPlan)
					assert.Equal(t, preview.RequestRequirements.Operation, preview.Entries[0].ConversionPlan.Operation)
				} else {
					assert.Empty(t, preview.CandidateChannelIDs)
					assert.Equal(t, RouteFilterPathUnsupported, preview.Entries[0].FilterReason)
				}
			})
		}
	}
}
