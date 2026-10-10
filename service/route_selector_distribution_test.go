package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoRouteOrderUsesPriorityThenLoadBucket(t *testing.T) {
	candidates := []RouteScoreCandidate{
		{ChannelID: 1, Priority: 20, Weight: 1, LoadRatio: .9, HealthUsable: true},
		{ChannelID: 2, Priority: 10, Weight: 100, LoadRatio: .2, HealthUsable: true},
		{ChannelID: 3, Priority: 10, Weight: 1, LoadRatio: .1, HealthUsable: true},
	}
	ordered := OrderAutoRouteCandidates(candidates, "bucket-request")
	require.Len(t, ordered, 3)
	assert.Equal(t, []int{1, 3, 2}, []int{ordered[0].Candidate.ChannelID, ordered[1].Candidate.ChannelID, ordered[2].Candidate.ChannelID})
	// Load buckets are 5% wide, so 10% load is bucket 2.
	assert.Equal(t, 2, ordered[1].Breakdown.LoadBucket)
	assert.Equal(t, 4, ordered[2].Breakdown.LoadBucket)
}

// A five-percent bucket separates occupancies that the previous ten-percent
// bucket collapsed, so a lightly loaded peer is not left to the weighted
// permutation against a noticeably busier one.
func TestAutoRouteOrderSeparatesNearbyLoadBuckets(t *testing.T) {
	candidates := []RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, LoadRatio: .11, HealthUsable: true},
		{ChannelID: 2, Priority: 10, Weight: 1, LoadRatio: .19, HealthUsable: true},
	}
	ordered := OrderAutoRouteCandidates(candidates, "nearby-load")
	require.Len(t, ordered, 2)
	assert.Equal(t, 2, ordered[0].Breakdown.LoadBucket)
	assert.Equal(t, 3, ordered[1].Breakdown.LoadBucket)
	assert.Equal(t, 1, ordered[0].Candidate.ChannelID)
}

// Zero-weight semantics follow NewAPI and are covered by
// TestOrderAutoRouteCandidatesFollowsNewAPIWeightSemantics.
func TestAutoRouteOrderIsStableAndWeighted(t *testing.T) {
	candidates := []RouteScoreCandidate{
		{ChannelID: 1, Weight: 1, HealthUsable: true},
		{ChannelID: 2, Weight: 3, HealthUsable: true},
	}
	counts := map[int]int{}
	for index := range 1000 {
		requestID := fmt.Sprintf("distribution-%d", index)
		ordered := OrderAutoRouteCandidates(candidates, requestID)
		counts[ordered[0].Candidate.ChannelID]++
	}
	// This fixed request corpus protects the configured 1:3 traffic-share
	// contract without randomness, timing assertions, or a throughput test.
	assert.InDelta(t, 250, counts[1], 50)
	assert.InDelta(t, 750, counts[2], 50)
	first := OrderAutoRouteCandidates(candidates, "stable-request")
	reversed := []RouteScoreCandidate{candidates[1], candidates[0]}
	assert.Equal(t, first, OrderAutoRouteCandidates(reversed, "stable-request"))
}

func TestRoutePlanningRetainsMoreThanThreeAndCapsDifferentChannels(t *testing.T) {
	input := RouteSelectionInput{SourceInput: RouteSourceInput{CapabilityEnabled: true}, RequestID: "many", DynamicScoringEnabled: true}
	for index := 1; index <= 70; index++ {
		input.AutoCandidates = append(input.AutoCandidates, RouteSelectionCandidate{ChannelID: index, HealthUsable: true, Weight: 1})
	}
	result, err := SelectTokenRoute(input)
	require.NoError(t, err)
	require.Len(t, result.Candidates, 64)
	selection := LiveRouteSelection{Attempts: selectedRouteAttemptCandidates(result)}
	for index := range selection.Attempts {
		if selection.Attempts[index].ChannelID != result.Candidates[3].ChannelID {
			selection.Attempts[index].FilterReason = "capacity_exhausted"
		}
	}
	candidate, _, found := selection.CandidateAtOrAfter(0)
	require.True(t, found)
	assert.Equal(t, result.Candidates[3].ChannelID, candidate.ChannelID)
	assert.False(t, DefaultRouteRetryBudget().Allows(ClassifyRouteError(503, "", "", "", false), RouteRetryFailover, RouteRetryCounters{TotalAttempts: 3}))
}

func TestManualRouteStickyOnlyWinsInsideFirstPositionLayer(t *testing.T) {
	input := RouteSelectionInput{
		SourceInput:        RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"},
		ManualGroupEnabled: true, ManualLoadBalance: true, RequestID: "sticky",
		ManualCandidates: []RouteSelectionCandidate{
			{ChannelID: 1, Position: 0, Weight: 100, HealthUsable: true},
			{ChannelID: 2, Position: 0, Weight: 1, Sticky: true, HealthUsable: true},
			{ChannelID: 3, Position: 1, Weight: 1000, Sticky: true, HealthUsable: true},
		},
	}
	result, err := SelectTokenRoute(input)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Decision.SelectedChannelID)
	input.ManualCandidates[1].FilterReason = "entry_disabled"
	result, err = SelectTokenRoute(input)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Decision.SelectedChannelID)
}
