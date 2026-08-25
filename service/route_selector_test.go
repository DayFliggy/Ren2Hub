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

func TestSelectTokenRouteAutoUsesPriorityLayerAndBoundedTopK(t *testing.T) {
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
	assert.Equal(t, []int{2, 3}, channelIDs(result.Candidates))
	assert.Equal(t, 2, result.Decision.SelectedChannelID)
	assert.NotNil(t, result.Decision.Candidates[0].Score)
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
