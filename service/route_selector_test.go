package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectTokenRouteKeepsLegacyFallbackWhenCapabilityIsDisabled(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: false, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualCandidates:   []RouteSelectionCandidate{{ChannelID: 1, HealthUsable: true}},
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceLegacy, result.Decision.RouteSource)
	assert.Empty(t, result.Candidates)
}

func TestSelectTokenRouteManualUsesActiveGroupPositionThenWeight(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualLoadBalance:  true,
		ManualCandidates: []RouteSelectionCandidate{
			{ChannelID: 9, Position: 1, Weight: 100, HealthUsable: true},
			{ChannelID: 8, Position: 0, Weight: 1, HealthUsable: true},
			{ChannelID: 7, Position: 0, Weight: 5, HealthUsable: true},
			{ChannelID: 6, Position: 0, Weight: 9, FilterReason: "channel_disabled", HealthUsable: true},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []int{7, 8, 9}, channelIDs(result.Candidates))
	assert.Equal(t, 7, result.Decision.SelectedChannelID)
}

func TestSelectTokenRouteManualLoadBalanceUsesRequestStablePositiveWeight(t *testing.T) {
	input := RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualLoadBalance:  true,
		RequestID:          "manual-weighted-request",
		ManualCandidates: []RouteSelectionCandidate{
			{ChannelID: 10, Position: 0, Weight: 0, HealthUsable: true},
			{ChannelID: 20, Position: 0, Weight: 1, HealthUsable: true},
			{ChannelID: 30, Position: 0, Weight: 9, HealthUsable: true},
			{ChannelID: 40, Position: 1, Weight: 100, HealthUsable: true},
		},
	}

	first, err := SelectTokenRoute(input)
	require.NoError(t, err)
	replayed, err := SelectTokenRoute(input)
	require.NoError(t, err)

	assert.Contains(t, []int{20, 30}, first.Decision.SelectedChannelID)
	assert.NotEqual(t, 10, first.Decision.SelectedChannelID, "zero weight must not win a weighted first attempt")
	assert.Equal(t, first.Decision.SelectedChannelID, replayed.Decision.SelectedChannelID)
	assert.Equal(t, 40, first.Candidates[len(first.Candidates)-1].ChannelID)
}

func TestSelectTokenRouteManualLoadBalanceTreatsAllZeroWeightsEqually(t *testing.T) {
	input := RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualLoadBalance:  true,
		RequestID:          "manual-zero-weight-request",
		ManualCandidates: []RouteSelectionCandidate{
			{ChannelID: 10, Position: 0, Weight: 0, HealthUsable: true},
			{ChannelID: 20, Position: 0, Weight: 0, HealthUsable: true},
		},
	}

	first, err := SelectTokenRoute(input)
	require.NoError(t, err)
	replayed, err := SelectTokenRoute(input)
	require.NoError(t, err)

	assert.Contains(t, []int{10, 20}, first.Decision.SelectedChannelID)
	assert.Equal(t, first.Decision.SelectedChannelID, replayed.Decision.SelectedChannelID)
}

func TestSelectTokenRouteAutoUsesPriorityLayerAndBoundedTopK(t *testing.T) {
	t.Setenv("ROUTE_SCORE_SHADOW_ENABLED", "true")
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput: RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:        10,
		AutoCandidates: []RouteSelectionCandidate{
			{ChannelID: 3, Priority: 20, Weight: 1, HealthUsable: true, ErrorRate: .1},
			{ChannelID: 2, Priority: 20, Weight: 3, HealthUsable: true, ErrorRate: .0},
			{ChannelID: 1, Priority: 10, Weight: 100, HealthUsable: true},
			{ChannelID: 4, Priority: 20, HealthUsable: false},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []int{2, 3, 1}, channelIDs(result.Candidates))
	assert.Equal(t, 2, result.Decision.SelectedChannelID)
	assert.Equal(t, "shadow", result.Decision.ScoringMode)
	assert.False(t, result.Decision.DynamicScoreApplied)
	for _, candidate := range result.Decision.Candidates {
		if candidate.FilterReason == "" {
			assert.NotNil(t, candidate.Score)
		}
	}
}

func TestSelectTokenRouteKeepsStaticOrderUntilScoreLiveIsEnabled(t *testing.T) {
	t.Setenv("ROUTE_SCORE_SHADOW_ENABLED", "true")
	input := RouteSelectionInput{
		SourceInput:  RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:         3,
		RequestModel: "gpt-5",
		AutoCandidates: []RouteSelectionCandidate{
			{ChannelID: 1, Priority: 10, Weight: 1, ErrorRate: 1, ErrorRateKnown: true, HealthUsable: true},
			{ChannelID: 2, Priority: 10, Weight: 1, ErrorRate: 0, ErrorRateKnown: true, HealthUsable: true},
		},
	}
	shadowResult, err := SelectTokenRoute(input)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, channelIDs(shadowResult.Candidates))
	assert.Equal(t, 1, shadowResult.Decision.SelectedChannelID)
	assert.Equal(t, 1, shadowResult.Decision.StaticPreferredChannelID)
	assert.Equal(t, 2, shadowResult.Decision.ScoredPreferredChannelID)

	input.DynamicScoringEnabled = true
	liveResult, err := SelectTokenRoute(input)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, channelIDs(liveResult.Candidates))
	assert.Equal(t, 2, liveResult.Decision.SelectedChannelID)
	assert.Equal(t, "live", liveResult.Decision.ScoringMode)
	assert.True(t, liveResult.Decision.DynamicScoreApplied)
}

func TestSelectTokenRouteScoreShadowRetainsEveryStaticPriorityLayer(t *testing.T) {
	t.Setenv("ROUTE_SCORE_SHADOW_ENABLED", "true")
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput: RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:        3,
		AutoCandidates: []RouteSelectionCandidate{
			{ChannelID: 30, Priority: 30, HealthUsable: true, ErrorRate: 1, ErrorRateKnown: true},
			{ChannelID: 20, Priority: 20, HealthUsable: true, ErrorRate: 0, ErrorRateKnown: true},
			{ChannelID: 10, Priority: 10, HealthUsable: true, ErrorRate: 0, ErrorRateKnown: true},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []int{30, 20, 10}, channelIDs(result.Candidates))
	assert.Equal(t, 30, result.Decision.SelectedChannelID)
	assert.Equal(t, 30, result.Decision.StaticPreferredChannelID)
	assert.Equal(t, 30, result.Decision.ScoredPreferredChannelID)
	assert.Equal(t, "shadow", result.Decision.ScoringMode)
}

func TestSelectTokenRouteFailsClosedWhenManualGroupHasNoEligibleCandidate(t *testing.T) {
	_, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualCandidates:   []RouteSelectionCandidate{{ChannelID: 1, FilterReason: "unknown_capability", HealthUsable: true}},
	})
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
}

func channelIDs(candidates []RouteSelectionCandidate) []int {
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ChannelID)
	}
	return ids
}
