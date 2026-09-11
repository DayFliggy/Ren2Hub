package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectTokenRouteRejectsDisabledCapability(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: false, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualCandidates:   []RouteSelectionCandidate{{ChannelID: 1, HealthUsable: true}},
	})
	require.ErrorIs(t, err, ErrRouteSelectionUnavailable)
	assert.Equal(t, RouteSourceUnavailable, result.Decision.RouteSource)
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
	assert.Equal(t, channelIDs(first.Candidates), channelIDs(replayed.Candidates), "the complete failover permutation must be replayable")
	assert.ElementsMatch(t, []int{10, 20, 30}, channelIDs(first.Candidates[:3]), "weighted sampling must not cross the manual position boundary")
	assert.NotEqual(t, 10, first.Candidates[0].ChannelID, "zero weight must follow positive weights in the same position layer")
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
	result, err := SelectTokenRoute(RouteSelectionInput{
		DynamicScoringEnabled: true,
		SourceInput:           RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:                  10,
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
	assert.Equal(t, "live", result.Decision.ScoringMode)
	assert.True(t, result.Decision.DynamicScoreApplied)
	for _, candidate := range result.Decision.Candidates {
		if candidate.FilterReason == "" {
			assert.NotNil(t, candidate.Score)
		}
	}
}

func TestSelectTokenRouteAutoKeepsTopKAndBoundedAdmissionReserves(t *testing.T) {
	candidates := make([]RouteSelectionCandidate, 0, 10)
	for channelID := 1; channelID <= 10; channelID++ {
		candidates = append(candidates, RouteSelectionCandidate{
			ChannelID: channelID, Priority: 10, Weight: 11 - channelID, HealthUsable: true,
		})
	}

	result, err := SelectTokenRoute(RouteSelectionInput{
		DynamicScoringEnabled: true,
		SourceInput:           RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:                  DefaultRouteCandidateTopK,
		AutoCandidates:        candidates,
	})
	require.NoError(t, err)
	require.Len(t, result.Candidates, DefaultRouteCandidateTopK)
	require.Len(t, result.ReserveCandidates, 3)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7}, channelIDs(result.Candidates))
	assert.Equal(t, []int{8, 9, 10}, channelIDs(result.ReserveCandidates))

	attempts := selectedRouteAttemptCandidates(result)
	require.Len(t, attempts, 20)
	assert.Equal(t, 10, attempts[len(attempts)-1].ChannelID, "admission must be able to reach a candidate beyond the initial TopK")
}

func TestSelectTokenRouteNeverPlansBeyondCandidateProbeBudget(t *testing.T) {
	candidates := make([]RouteSelectionCandidate, 0, 70)
	for channelID := 1; channelID <= 70; channelID++ {
		candidates = append(candidates, RouteSelectionCandidate{
			ChannelID: channelID, Priority: 10, Weight: 1, HealthUsable: true,
		})
	}

	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput:    RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:           DefaultRouteCandidateTopK,
		AutoCandidates: candidates,
	})
	require.NoError(t, err)
	assert.Len(t, result.Candidates, DefaultRouteCandidateTopK)
	assert.Len(t, result.ReserveCandidates, 64-DefaultRouteCandidateTopK)
}

func TestSelectTokenRouteScoringRespectsObservedHealth(t *testing.T) {
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
	assert.Zero(t, shadowResult.Decision.ScoredPreferredChannelID)

	input.DynamicScoringEnabled = true
	liveResult, err := SelectTokenRoute(input)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, channelIDs(liveResult.Candidates))
	assert.Equal(t, 2, liveResult.Decision.SelectedChannelID)
	assert.Equal(t, "live", liveResult.Decision.ScoringMode)
	assert.True(t, liveResult.Decision.DynamicScoreApplied)
}

func TestSelectTokenRouteExhaustsNativeTierBeforeCompatibleCandidates(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		DynamicScoringEnabled: true,
		RequestID:             "native-first",
		SourceInput:           RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		TopK:                  3,
		AutoCandidates: []RouteSelectionCandidate{
			{ChannelID: 1, Priority: 100, Weight: 100, CompatibilityTier: 1, HealthUsable: true},
			{ChannelID: 2, Priority: 1, Weight: 1, CompatibilityTier: 0, HealthUsable: true},
			{ChannelID: 3, Priority: 1, Weight: 1, CompatibilityTier: 0, HealthUsable: true},
		},
	})

	require.NoError(t, err)
	assert.ElementsMatch(t, []int{2, 3}, channelIDs(result.Candidates[:2]))
	assert.Equal(t, 1, result.Candidates[2].ChannelID)
}

func TestManualRouteExhaustsNativeTierBeforeConfiguredPosition(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		RequestID:          "manual-native-first",
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualLoadBalance:  true,
		ManualCandidates: []RouteSelectionCandidate{
			{ChannelID: 1, Position: 0, Weight: 100, CompatibilityTier: 1, HealthUsable: true},
			{ChannelID: 2, Position: 9, Weight: 1, CompatibilityTier: 0, HealthUsable: true},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, channelIDs(result.Candidates))
}

func TestSelectTokenRouteManualStickyWinsInsidePositionLayer(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		RequestID:          "manual-sticky",
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true,
		ManualLoadBalance:  true,
		ManualCandidates: []RouteSelectionCandidate{
			{ChannelID: 1, Position: 0, Weight: 1000, HealthUsable: true},
			{ChannelID: 2, Position: 0, Weight: 1, Sticky: true, HealthUsable: true},
			{ChannelID: 3, Position: 1, Weight: 1000, Sticky: true, HealthUsable: true},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []int{2, 1, 3}, channelIDs(result.Candidates))
}

func TestSelectTokenRouteAutoStickyWinsInsidePriorityLayer(t *testing.T) {
	result, err := SelectTokenRoute(RouteSelectionInput{
		DynamicScoringEnabled: true,
		RequestID:             "auto-sticky",
		SourceInput:           RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"},
		AutoCandidates: []RouteSelectionCandidate{
			{ChannelID: 1, Priority: 10, Weight: 1000, ErrorRate: 0, ErrorRateKnown: true, HealthUsable: true},
			{ChannelID: 2, Priority: 10, Weight: 1, ErrorRate: 1, ErrorRateKnown: true, Sticky: true, HealthUsable: true},
			{ChannelID: 3, Priority: 5, Weight: 1000, Sticky: true, HealthUsable: true},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, 2, result.Candidates[0].ChannelID)
	assert.Equal(t, 3, result.Candidates[len(result.Candidates)-1].ChannelID)
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
