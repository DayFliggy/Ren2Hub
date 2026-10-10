package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

// A degraded channel must lose to a healthy peer inside the same priority
// layer, because the implicit weight permutation can otherwise select it on
// luck. Priority itself stays absolute.
func TestOrderAutoRouteCandidatesPrefersHealthyPeerWithinPriority(t *testing.T) {
	scored := OrderAutoRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandGood, ErrorBandKnown: true},
	}, "request-1")
	require.Len(t, scored, 2)
	assert.Equal(t, 2, scored[0].Candidate.ChannelID)
	assert.Equal(t, 1, scored[1].Candidate.ChannelID)

	// The same degraded channel outranks a healthy lower priority layer.
	scored = OrderAutoRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 20, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandGood, ErrorBandKnown: true},
	}, "request-1")
	require.Len(t, scored, 2)
	assert.Equal(t, 1, scored[0].Candidate.ChannelID)
}

// A channel below the sample floor competes as a healthy peer: it takes its
// weight share of first attempts so its band can become known, it outranks a
// known critical peer, and priority still bounds it.
func TestOrderAutoRouteCandidatesGivesColdChannelItsWeightShare(t *testing.T) {
	candidates := []RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandGood, ErrorBandKnown: true, TTFTBand: RouteRankBandGood, TTFTBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 1, HealthUsable: true},
	}
	counts := map[int]int{}
	for index := range 1000 {
		ordered := OrderAutoRouteCandidates(candidates, fmt.Sprintf("cold-%d", index))
		counts[ordered[0].Candidate.ChannelID]++
	}
	assert.InDelta(t, 500, counts[2], 50)

	scored := OrderAutoRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 1, HealthUsable: true},
	}, "request-1")
	assert.Equal(t, 2, scored[0].Candidate.ChannelID)

	scored = OrderAutoRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 20, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 1, HealthUsable: true},
	}, "request-1")
	assert.Equal(t, 1, scored[0].Candidate.ChannelID)
}

// A channel without the minimum sample carries no band, so it is never
// reordered on the strength of an unrepresentative observation.
func TestRouteHealthRankingMetricsRequiresMinimumSample(t *testing.T) {
	insufficient := model.ChannelHealth{ID: 1, ObservedRequests: RouteRankMinimumSamples - 1, ObservedFailures: RouteRankMinimumSamples - 1}
	rank := RouteHealthRankingMetrics(insufficient)
	assert.False(t, rank.ErrorBandKnown)
	assert.False(t, rank.TTFTBandKnown)

	critical := model.ChannelHealth{ID: 1, ObservedRequests: 100, ObservedFailures: 40, TTFTObservationCnt: 100, TTFTSumMS: 2_000_000}
	rank = RouteHealthRankingMetrics(critical)
	require.True(t, rank.ErrorBandKnown)
	assert.Equal(t, RouteRankBandCritical, rank.ErrorBand)
	require.True(t, rank.TTFTBandKnown)
	assert.Equal(t, RouteRankBandCritical, rank.TTFTBand)

	healthy := model.ChannelHealth{ID: 1, ObservedRequests: 100, ObservedFailures: 1, TTFTObservationCnt: 100, TTFTSumMS: 100_000}
	rank = RouteHealthRankingMetrics(healthy)
	require.True(t, rank.ErrorBandKnown)
	assert.Equal(t, RouteRankBandGood, rank.ErrorBand)
	assert.Equal(t, RouteRankBandGood, rank.TTFTBand)
}

// Key-scoped rows are written by failure-only paths, so their counters cannot
// express a rate and must not produce a band.
func TestRouteHealthRankingMetricsIgnoresKeyScopedRows(t *testing.T) {
	rank := RouteHealthRankingMetrics(model.ChannelHealth{
		ID: 1, KeyScope: "key:abc", ObservedRequests: 500, ObservedFailures: 500,
	})
	assert.False(t, rank.ErrorBandKnown)
	assert.False(t, rank.TTFTBandKnown)
}

func TestRouteHealthRankingMetricsBoundsErrorAndTTFTBands(t *testing.T) {
	cases := []struct {
		name     string
		requests int64
		failures int64
		wantBand RouteRankBand
	}{
		{name: "at warning floor", requests: 100, failures: 5, wantBand: RouteRankBandWarning},
		{name: "below warning floor", requests: 100, failures: 4, wantBand: RouteRankBandGood},
		{name: "at critical floor", requests: 100, failures: 20, wantBand: RouteRankBandCritical},
		{name: "all failing", requests: 100, failures: 100, wantBand: RouteRankBandCritical},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rank := RouteHealthRankingMetrics(model.ChannelHealth{ID: 1, ObservedRequests: testCase.requests, ObservedFailures: testCase.failures})
			require.True(t, rank.ErrorBandKnown)
			assert.Equal(t, testCase.wantBand, rank.ErrorBand)
		})
	}
}

// Health orders candidates through the hard band keys, so it never rescales
// the configured weight: equally degraded peers keep their 1:3 ratio instead
// of the 1:2 that rounding a 0.5 factor produced.
func TestOrderAutoRouteCandidatesKeepsWeightRatioInsideDegradedBand(t *testing.T) {
	candidates := []RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 1, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 3, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
	}
	firsts := map[int]int{}
	const samples = 4000
	for request := range samples {
		ordered := OrderAutoRouteCandidates(candidates, fmt.Sprintf("degraded-%d", request))
		require.Len(t, ordered, 2)
		assert.Equal(t, ordered[0].Candidate.Weight, ordered[0].Breakdown.EffectiveWeight)
		firsts[ordered[0].Candidate.ChannelID]++
	}
	assert.InDelta(t, 0.25, float64(firsts[1])/samples, 0.04)
	assert.InDelta(t, 0.75, float64(firsts[2])/samples, 0.04)
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

// Inside one tie group the configured weight follows NewAPI
// (model/channel_cache.go GetRandomSatisfiedChannel): first attempts are
// proportional to weight, a zero-weight channel gets none while a weighted peer
// exists, and an all-zero group shares equally. Zero-weight channels stay in
// the order as failover candidates.
func TestOrderAutoRouteCandidatesFollowsNewAPIWeightSemantics(t *testing.T) {
	cases := []struct {
		name        string
		weights     []int
		wantShare   []float64
		sampleCount int
	}{
		{name: "zero weight yields to a weighted peer", weights: []int{0, 100}, wantShare: []float64{0, 1}, sampleCount: 2000},
		{name: "zero weight yields even to weight one", weights: []int{0, 1}, wantShare: []float64{0, 1}, sampleCount: 2000},
		{name: "all zero weights share equally", weights: []int{0, 0}, wantShare: []float64{0.5, 0.5}, sampleCount: 2000},
		{name: "positive weights stay proportional", weights: []int{1, 3}, wantShare: []float64{0.25, 0.75}, sampleCount: 4000},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			candidates := make([]RouteScoreCandidate, 0, len(testCase.weights))
			for index, weight := range testCase.weights {
				candidates = append(candidates, RouteScoreCandidate{ChannelID: index + 1, Priority: 10, Weight: weight, HealthUsable: true})
			}
			firsts := map[int]int{}
			for request := range testCase.sampleCount {
				ordered := OrderAutoRouteCandidates(candidates, fmt.Sprintf("weight-%d", request))
				require.Len(t, ordered, len(candidates), "zero-weight channels remain failover candidates")
				firsts[ordered[0].Candidate.ChannelID]++
			}
			for index, want := range testCase.wantShare {
				if want == 0 {
					assert.Zero(t, firsts[index+1], "channel %d", index+1)
					continue
				}
				share := float64(firsts[index+1]) / float64(testCase.sampleCount)
				assert.InDelta(t, want, share, 0.04, "channel %d", index+1)
			}
		})
	}
}

// A zero-weight channel is a backup: load on its weighted peer does not hand
// it first attempts, and neither does soft affinity. Only a worse error band
// on the weighted peer does, because health is compared first.
func TestOrderAutoRouteCandidatesKeepsZeroWeightBehindLoadedWeightedPeer(t *testing.T) {
	for _, load := range []float64{0.05, 0.5, 0.95} {
		candidates := []RouteScoreCandidate{
			{ChannelID: 1, Priority: 10, Weight: 100, HealthUsable: true, LoadRatio: load},
			{ChannelID: 2, Priority: 10, Weight: 0, HealthUsable: true, Sticky: true},
		}
		for request := range 500 {
			ordered := OrderAutoRouteCandidates(candidates, fmt.Sprintf("loaded-%d", request))
			require.Len(t, ordered, 2)
			require.Equal(t, 1, ordered[0].Candidate.ChannelID, "load %.2f request %d", load, request)
			require.Equal(t, 2, ordered[1].Candidate.ChannelID, "the zero-weight channel stays a failover candidate")
		}
	}

	ordered := OrderAutoRouteCandidates([]RouteScoreCandidate{
		{ChannelID: 1, Priority: 10, Weight: 100, HealthUsable: true, ErrorBand: RouteRankBandCritical, ErrorBandKnown: true},
		{ChannelID: 2, Priority: 10, Weight: 0, HealthUsable: true},
	}, "degraded-weighted-peer")
	assert.Equal(t, 2, ordered[0].Candidate.ChannelID)
}
