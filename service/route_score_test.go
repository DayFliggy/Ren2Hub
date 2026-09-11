package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreRouteCandidatesNeverLetsDynamicScoreCrossPriority(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 20, Weight: 1, ErrorRate: 0.9, HealthUsable: true},
		{ChannelID: 2, Priority: 10, Weight: 100000, ErrorRate: 0, HealthUsable: true},
		{ChannelID: 3, Priority: 20, Weight: 10, ErrorRate: 0, HealthUsable: true},
	})
	require.Len(t, scored, 3)
	assert.Equal(t, 3, scored[0].Candidate.ChannelID)
	assert.Equal(t, int64(20), scored[0].Breakdown.PriorityLayer)
	assert.Equal(t, 1, scored[1].Candidate.ChannelID)
	assert.Equal(t, 2, scored[2].Candidate.ChannelID)
	assert.Equal(t, int64(10), scored[2].Breakdown.PriorityLayer)
}

func TestScoreRouteCandidatesUsesStableChannelTieBreakerAndHardHealthFilter(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 5, Priority: 1, Weight: 10, HealthUsable: true},
		{ChannelID: 4, Priority: 1, Weight: 10, HealthUsable: true},
		{ChannelID: 3, Priority: 1, Weight: 100, HealthUsable: false},
	})
	assert.Equal(t, []int{4, 5}, []int{scored[0].Candidate.ChannelID, scored[1].Candidate.ChannelID})
}

func TestScoreRouteCandidatesFallsBackAfterHealthFiltersHighestPriority(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 20, Weight: 100, HealthUsable: false},
		{ChannelID: 2, Priority: 10, Weight: 1, HealthUsable: true},
	})

	if assert.Len(t, scored, 1) {
		assert.Equal(t, 2, scored[0].Candidate.ChannelID)
		assert.Equal(t, int64(10), scored[0].Breakdown.PriorityLayer)
	}
}

func TestScoreRouteCandidatesIgnoresUnhealthyWeightAndBoundsTopK(t *testing.T) {
	// More healthy candidates than the budget, so the bound actually clamps.
	// With only as many candidates as the limit the assertion would pass
	// regardless of the limit and stop protecting anything.
	candidates := []RouteScoreCandidate{
		{ChannelID: 2, Priority: 10, Weight: 1000, HealthUsable: false},
	}
	for id := range DefaultRouteCandidateTopK + 2 {
		candidates = append(candidates, RouteScoreCandidate{
			ChannelID: 100 + id, Priority: 10, Weight: 100 - id, HealthUsable: true,
		})
	}

	scored := TopKRouteCandidates(candidates, 99)
	require.Len(t, scored, DefaultRouteCandidateTopK)
	assert.Equal(t, 1.0, scored[0].Breakdown.WeightScore)
	for _, candidate := range scored {
		assert.NotEqual(t, 2, candidate.Candidate.ChannelID, "an unhealthy candidate must never be scored")
	}

	// A caller may still ask for fewer than the budget.
	assert.Len(t, TopKRouteCandidates(candidates, 2), 2)
}

func TestScoreRouteCandidatesTreatsUnknownMetricsAsNeutralAndExplicit(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{{
		ChannelID: 1, Priority: 10, Weight: 1, HealthUsable: true,
	}})
	require.Len(t, scored, 1)
	breakdown := scored[0].Breakdown
	assert.False(t, breakdown.ErrorKnown)
	assert.False(t, breakdown.LatencyKnown)
	assert.False(t, breakdown.TTFTKnown)
	assert.False(t, breakdown.RateLimitKnown)
	assert.False(t, breakdown.QuotaKnown)
	assert.Equal(t, unknownRouteMetricScore, breakdown.ErrorScore)
	assert.Equal(t, unknownRouteMetricScore, breakdown.LatencyScore)
	assert.Equal(t, unknownRouteMetricScore, breakdown.RateLimitScore)
}

func TestScoreRouteCandidatesUsesKnownMetricsOnlyInsidePriorityLayer(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, ErrorRate: 1, ErrorRateKnown: true, HealthUsable: true},
		{ChannelID: 2, Priority: 10, Weight: 1, ErrorRate: 0, ErrorRateKnown: true, HealthUsable: true},
		{ChannelID: 3, Priority: 5, Weight: 100, ErrorRate: 0, ErrorRateKnown: true, Sticky: true, HealthUsable: true},
	})
	require.Len(t, scored, 3)
	assert.Equal(t, []int{2, 1, 3}, []int{
		scored[0].Candidate.ChannelID,
		scored[1].Candidate.ChannelID,
		scored[2].Candidate.ChannelID,
	})
}

func TestOrderManualRouteCandidatesKeepsPositionAheadOfWeight(t *testing.T) {
	ordered := OrderManualRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 9, Position: 1, Weight: 100},
		{ChannelID: 7, Position: 0, Weight: 1},
		{ChannelID: 8, Position: 0, Weight: 10},
	}, true)
	assert.Equal(t, []int{8, 7, 9}, []int{ordered[0].ChannelID, ordered[1].ChannelID, ordered[2].ChannelID})
	ordered = OrderManualRouteCandidates(ordered, false)
	assert.Equal(t, []int{7, 8, 9}, []int{ordered[0].ChannelID, ordered[1].ChannelID, ordered[2].ChannelID})
}
