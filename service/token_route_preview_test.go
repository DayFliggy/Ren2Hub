package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenPreviewLegacyAutoOwnershipAndNoReservations(t *testing.T) {
	db := setupRouteProfileTest(t)
	user, token, channel := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channel, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	input := RouteProfilePreviewInput{Model: "gpt-test", Path: "/v1/chat/completions"}
	before := ReadGatewayAdmissionRuntime(ChannelAdmissionKey(channel))
	preview, err := PreviewUserTokenRoute(context.Background(), user, token, input)
	require.NoError(t, err)
	assert.Equal(t, model.RouteModeAutoLab, preview.Mode)
	assert.Zero(t, preview.ProfileID)
	assert.Equal(t, []int{channel}, preview.CandidateChannelIDs)
	assert.NotEmpty(t, preview.PreviewID)
	assert.NotZero(t, preview.GeneratedAt)
	assert.Equal(t, before, ReadGatewayAdmissionRuntime(ChannelAdmissionKey(channel)))
	var profiles int64
	require.NoError(t, db.Model(&model.UserRouteProfile{}).Count(&profiles).Error)
	assert.Zero(t, profiles, "preview must not create an automatic profile")
	_, err = PreviewUserTokenRoute(context.Background(), user+1, token, input)
	assert.ErrorIs(t, err, ErrRouteProfileNotFound)
	input.Request = []byte(`{"model":"other-model"}`)
	_, err = PreviewUserTokenRoute(context.Background(), user, token, input)
	assert.ErrorIs(t, err, ErrRouteProfileValidation)
}

func TestManualRetryScopeStartsAfterFirstDispatch(t *testing.T) {
	for _, mode := range []string{model.RoutePolicyRetryNone, model.RoutePolicyRetrySameChannel} {
		selection := LiveRouteSelection{Retry: RouteLiveRetryPolicy{Mode: mode, MaxSameResourceAttempts: 1}}
		class := RouteErrorClassification{Retryable: true, Failoverable: true}
		assert.True(t, selection.AllowsCandidate(2, 0, class, RouteRetryCounters{}))
		assert.False(t, selection.AllowsCandidate(3, 2, class, RouteRetryCounters{TotalAttempts: 1}))
		assert.Equal(t, mode == model.RoutePolicyRetrySameChannel, selection.AllowsCandidate(2, 2, class, RouteRetryCounters{TotalAttempts: 1}))
		assert.False(t, selection.AllowsCandidate(2, 2, class, RouteRetryCounters{TotalAttempts: 2, SameChannelAttempts: 1}))
	}
}

func TestPreviewGeminiPathMustMatchRequestModel(t *testing.T) {
	_, err := AnalyzeRouteRequest("gemini-a", "/v1beta/models/gemini-b:generateContent", []byte(`{}`))
	require.ErrorContains(t, err, "path model")
	_, err = AnalyzeRouteRequest("gemini-a", "/v1beta/models/gemini-a:unsupported", []byte(`{}`))
	require.ErrorContains(t, err, "request path")
	requirements, err := AnalyzeRouteRequest("gemini-a", "/v1beta/models/gemini-a:streamGenerateContent", []byte(`{}`))
	require.NoError(t, err)
	assert.True(t, requirements.Stream)
}

func TestRoutePreviewRejectsUnregisteredPathLookalikes(t *testing.T) {
	for _, path := range []string{
		"/v1/chat/completions-extra", "/v1/responses/compact-extra",
		"/v1/messages/count_tokens", "/v1/audio/unknown",
		"/v1/models-extra/models/gpt-test:generateContent",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := AnalyzeRouteRequest("gpt-test", path, []byte(`{}`))
			require.Error(t, err)
		})
	}
}

func TestTokenPreviewUsesOrdinaryCompactModelAndCapability(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "auto", true: "manual"}[manual], func(t *testing.T) {
			db := setupRouteProfileTest(t)
			userID, tokenID, channelID := seedRouteProfileFixture(t, db)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("models", "gpt-5").Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channelID).Update("model", "gpt-5").Error)
			require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, "compact-preview", "preview-catalog", []model.ChannelModelCapability{{
				RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", Confidence: 1,
				AbilityGroups: `["default"]`, EndpointTypes: `["openai-response-compact"]`, ChannelStatus: 1,
				ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
			}}))
			require.NoError(t, RebuildRouteCapabilityIndex(context.Background()))
			if manual {
				_, err := CreateUserRouteProfile(RouteProfileInput{UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
					Groups: []RouteGroupInput{{Name: "compact", Enabled: true, Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}}}},
				})
				require.NoError(t, err)
			}
			input := RouteProfilePreviewInput{Model: "gpt-5", Path: "/v1/responses/compact"}
			preview, err := PreviewUserTokenRoute(context.Background(), userID, tokenID, input)
			require.NoError(t, err)
			assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
			entry := findRoutePreviewEntry(t, preview, channelID)
			assert.Equal(t, "gpt-5", entry.RequestModel)
			assert.Equal(t, "gpt-5", entry.ActualModel)
			assert.Equal(t, model.RouteCapabilityStateEligible, entry.CapabilityState)
			assert.Empty(t, entry.FilterReason)
			selection, err := SelectLiveTokenRoute(LiveRouteRequest{Context: context.Background(), Preview: true, CapabilityEnabled: true,
				UserID: userID, TokenID: tokenID, RequestID: preview.PreviewID, RequestModel: input.Model, RequestPath: input.Path,
				Requirements: &preview.RequestRequirements, ConversionPolicy: preview.ConversionPolicy,
			})
			require.NoError(t, err)
			assert.Equal(t, preview.PreferredChannelID, selection.Decision.SelectedChannelID)
			for _, allowedModel := range []string{"gpt-5", "other-model"} {
				require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{"model_limits_enabled": true, "model_limits": allowedModel}).Error)
				limited, err := PreviewUserTokenRoute(context.Background(), userID, tokenID, input)
				require.NoError(t, err)
				if allowedModel == "gpt-5" {
					assert.Equal(t, []int{channelID}, limited.CandidateChannelIDs)
				} else {
					assert.Empty(t, limited.CandidateChannelIDs)
				}
			}
		})
	}
}

func TestTokenPreviewReportsRequestWideDisabledState(t *testing.T) {
	for _, disabled := range []string{"profile", "user", "token"} {
		t.Run(disabled, func(t *testing.T) {
			db := setupRouteProfileTest(t)
			userID, tokenID, channelID := seedRouteProfileFixture(t, db)
			publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
			reason := RouteFilterTokenForbidden
			switch disabled {
			case "profile":
				profile, err := CreateUserRouteProfile(RouteProfileInput{UserID: userID, TokenID: tokenID, Mode: model.RouteModeAutoLab})
				require.NoError(t, err)
				require.NoError(t, db.Model(&model.UserRouteProfile{}).Where("id = ?", profile.Profile.ID).Update("status", model.RouteProfileStatusDisabled).Error)
				reason = RoutePreviewFilterProfileDisabled
			case "user":
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("status", common.UserStatusDisabled).Error)
			case "token":
				require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("status", common.TokenStatusDisabled).Error)
			}
			preview, err := PreviewUserTokenRoute(context.Background(), userID, tokenID, RouteProfilePreviewInput{Model: "gpt-test", Path: "/v1/chat/completions"})
			require.NoError(t, err)
			assert.Empty(t, preview.CandidateChannelIDs)
			assert.Zero(t, preview.PreferredChannelID)
			assert.Positive(t, preview.FilterReasonCounts[reason])
		})
	}
}

func TestManualPreviewDoesNotClaimEligibilityWithoutDispatchDiscovery(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	// Capture an index before a channel's published snapshot becomes visible.
	require.NoError(t, RebuildRouteCapabilityIndex(context.Background()))
	staleIndex := routeCapabilityIndex.Load()
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	routeCapabilityIndex.Store(staleIndex)
	_, err := CreateUserRouteProfile(RouteProfileInput{UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "manual", Enabled: true, Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}}}},
	})
	require.NoError(t, err)
	preview, err := PreviewUserTokenRoute(context.Background(), userID, tokenID, RouteProfilePreviewInput{Model: "gpt-test", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	assert.Empty(t, preview.CandidateChannelIDs)
	entry := findRoutePreviewEntry(t, preview, channelID)
	assert.Equal(t, RouteFilterSnapshotStale, entry.FilterReason)
	assert.Equal(t, 1, preview.FilterReasonCounts[RouteFilterSnapshotStale])
}
