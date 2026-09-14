package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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
	assert.Equal(t, 1, scored[0].Candidate.ChannelID)
	assert.Equal(t, int64(20), scored[0].Breakdown.PriorityLayer)
	assert.Equal(t, 3, scored[1].Candidate.ChannelID)
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

func TestScoreRouteCandidatesIgnoresUnhealthyCandidates(t *testing.T) {
	candidates := []RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 100, HealthUsable: true},
		{ChannelID: 2, Priority: 10, Weight: 1000, HealthUsable: false},
		{ChannelID: 3, Priority: 10, Weight: 50, HealthUsable: true},
		{ChannelID: 4, Priority: 10, Weight: 25, HealthUsable: true},
	}
	scored := ScoreRouteCandidates(candidates)
	require.Len(t, scored, 3)
	assert.Equal(t, 100, scored[0].Breakdown.EffectiveWeight)
	for _, candidate := range scored {
		assert.NotEqual(t, 2, candidate.Candidate.ChannelID)
	}
}

func TestRouteDiagnosticsDistinguishUnknownAndMeasuredZero(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{{
		ChannelID: 1, Priority: 10, Weight: 1, HealthUsable: true,
	}})
	require.Len(t, scored, 1)
	breakdown := scored[0].Breakdown
	encoded, err := common.Marshal(breakdown)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, common.Unmarshal(encoded, &fields))
	for _, name := range []string{"error_rate", "latency_ms", "ttft_ms", "rate_limit_headroom", "quota_headroom", "total"} {
		assert.NotContains(t, fields, name)
	}
	scored = ScoreRouteCandidates([]RouteScoreCandidate{{ChannelID: 1, HealthUsable: true, ErrorRateKnown: true, LatencyKnown: true}})
	require.Len(t, scored, 1)
	encoded, err = common.Marshal(scored[0].Breakdown)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encoded, &fields))
	assert.Equal(t, float64(0), fields["error_rate"])
	assert.Equal(t, float64(0), fields["latency_ms"])
}

func TestScoreRouteCandidatesKeepsMetricsObservational(t *testing.T) {
	scored := ScoreRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, ErrorRate: 1, ErrorRateKnown: true, HealthUsable: true},
		{ChannelID: 2, Priority: 10, Weight: 1, ErrorRate: 0, ErrorRateKnown: true, HealthUsable: true},
		{ChannelID: 3, Priority: 5, Weight: 100, ErrorRate: 0, ErrorRateKnown: true, Sticky: true, HealthUsable: true},
	})
	require.Len(t, scored, 3)
	assert.Equal(t, []int{1, 2, 3}, []int{
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
