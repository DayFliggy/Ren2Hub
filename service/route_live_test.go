package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectLiveTokenRouteManualUsesOnlyActiveGroup(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-manual",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceManual, selection.Source)
	assert.Equal(t, channelID, selection.Decision.SelectedChannelID)
	assert.Equal(t, created.Profile.Version, selection.Decision.ConfigurationVersion)
}

func TestSelectLiveTokenRouteAutoLabUsesActiveIndexAndCurrentChannelStatus(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	channel := model.Channel{}
	require.NoError(t, db.First(&channel, "id = ?", channelID).Error)
	abilityGroups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	capability := model.ChannelModelCapability{
		ChannelID: channelID, SnapshotVersion: 1, RequestModel: "gpt-test", ActualModel: "gpt-test",
		LabSlug: "openai", Source: "canonical", State: model.RouteCapabilityStateEligible,
		AbilityGroups: string(abilityGroups), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, EndpointTypes: `[
  "openai"
]`, Priority: 20, Weight: 10,
	}
	resetRouteCapabilityIndexForTest()
	routeCapabilityIndex.Store(&capabilityIndex{
		ByRequestModel: map[string][]indexedCapability{"gpt-test": {{
			Capability: capability, ChannelStatus: common.ChannelStatusEnabled,
			Priority: 20, Weight: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
		}}},
	})
	t.Cleanup(resetRouteCapabilityIndexForTest)
	_, err = CreateUserRouteProfile(RouteProfileInput{UserID: userID, TokenID: tokenID, Mode: model.RouteModeAutoLab})
	require.NoError(t, err)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-auto",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceAutoLab, selection.Source)
	assert.Equal(t, channelID, selection.Decision.SelectedChannelID)

	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, err = SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-auto-disabled",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default",
	})
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
}

func TestSelectLiveTokenRouteMissingProfileKeepsLegacy(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, _ := seedRouteProfileFixture(t, db)
	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, UserID: userID, TokenID: tokenID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceLegacy, selection.Source)
	assert.Empty(t, selection.Decision.SelectedChannelID)
}

func TestSelectLiveTokenRouteFiltersOpenChannelModelHealth(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "health", Enabled: true, Entries: []RouteEntryInput{{
			ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
		}}}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: "", State: model.RouteHealthStateOpen,
		FailureCount: 3, CooldownUntil: time.Now().Add(time.Hour).Unix(), HealthEpoch: 2,
	}).Error)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-health",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
	})
	assert.Equal(t, RouteSourceManual, selection.Source)
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
	assert.Equal(t, created.Profile.Version, selection.Decision.ConfigurationVersion)
	assert.Equal(t, RouteCandidateFilterHealthUnavailable, selection.Decision.Candidates[0].FilterReason)
}

func TestLiveRouteSelectionMaxRatioOnlyRestrictsManualRoutes(t *testing.T) {
	manual := LiveRouteSelection{Source: RouteSourceManual, MaxRatio: 1.5}
	assert.True(t, manual.AllowsPriceRatio(1.5))
	assert.False(t, manual.AllowsPriceRatio(1.500001))
	assert.False(t, manual.AllowsPriceRatio(-1))

	auto := LiveRouteSelection{Source: RouteSourceAutoLab, MaxRatio: 1}
	assert.True(t, auto.AllowsPriceRatio(3))
	legacy := LiveRouteSelection{Source: RouteSourceLegacy, MaxRatio: 1}
	assert.True(t, legacy.AllowsPriceRatio(3))
}

func TestManualRouteAttemptsHonorPolicyWithoutExpandingSystemLimit(t *testing.T) {
	result := RouteSelectionResult{Candidates: []RouteSelectionCandidate{
		{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3},
	}, Decision: RouteDecision{Candidates: []RouteDecisionCandidate{
		{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3},
	}}}
	channelIDs := func(attempts []RouteDecisionCandidate) []int {
		ids := make([]int, 0, len(attempts))
		for _, attempt := range attempts {
			ids = append(ids, attempt.ChannelID)
		}
		return ids
	}
	assert.Equal(t, []int{1}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{Mode: model.RoutePolicyRetryNone})))
	assert.Equal(t, []int{1, 1, 1}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{
		Mode: model.RoutePolicyRetrySameChannel, MaxSameResourceAttempts: 9,
	})))
	assert.Equal(t, []int{1, 2}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{
		Mode: model.RoutePolicyRetryNextChannel, MaxFailoverAttempts: 1,
	})))
	assert.Equal(t, []int{1, 1, 2}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{
		Mode: model.RoutePolicyRetrySameThenNext, MaxSameResourceAttempts: 1, MaxFailoverAttempts: 2,
	})))
}
