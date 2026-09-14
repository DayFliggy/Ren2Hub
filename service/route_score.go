package service

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"strconv"
)

type RouteScoreCandidate struct {
	ChannelID         int
	Priority          int64
	Position          int
	Weight            int
	ErrorRate         float64
	ErrorRateKnown    bool
	LatencyMS         float64
	LatencyKnown      bool
	TTFTMS            float64
	TTFTKnown         bool
	RateLimitHeadroom float64
	RateLimitKnown    bool
	QuotaHeadroom     float64
	QuotaKnown        bool
	Sticky            bool
	HealthUsable      bool
	LoadRatio         float64
}

type RouteScoreBreakdown struct {
	PriorityLayer     int64    `json:"priority_layer"`
	EffectiveWeight   int      `json:"effective_weight"`
	LoadRatio         float64  `json:"load_ratio"`
	LoadBucket        int      `json:"load_bucket"`
	Sticky            bool     `json:"sticky"`
	SelectionRank     float64  `json:"selection_rank"`
	ErrorRate         *float64 `json:"error_rate,omitempty"`
	LatencyMS         *float64 `json:"latency_ms,omitempty"`
	TTFTMS            *float64 `json:"ttft_ms,omitempty"`
	RateLimitHeadroom *float64 `json:"rate_limit_headroom,omitempty"`
	QuotaHeadroom     *float64 `json:"quota_headroom,omitempty"`
}

type ScoredRouteCandidate struct {
	Candidate RouteScoreCandidate
	Breakdown RouteScoreBreakdown
}

// OrderAutoRouteCandidates keeps priority and ten-percent load buckets hard.
// Weighted rendezvous gives each request a stable permutation without shared
// random state; a zero configured weight still receives a minimum share of one.
func OrderAutoRouteCandidates(candidates []RouteScoreCandidate, requestID string) []ScoredRouteCandidate {
	ordered := ScoreRouteCandidates(candidates)
	for index := range ordered {
		candidate := ordered[index].Candidate
		digest := sha256.Sum256([]byte(requestID + ":" + strconv.Itoa(candidate.ChannelID)))
		uniform := (float64(binary.BigEndian.Uint64(digest[:8])>>11) + 1) / (float64(uint64(1)<<53) + 1)
		ordered[index].Breakdown.SelectionRank = -math.Log(uniform) / float64(ordered[index].Breakdown.EffectiveWeight)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Candidate.Priority != right.Candidate.Priority {
			return left.Candidate.Priority > right.Candidate.Priority
		}
		if left.Breakdown.LoadBucket != right.Breakdown.LoadBucket {
			return left.Breakdown.LoadBucket < right.Breakdown.LoadBucket
		}
		if left.Candidate.Sticky != right.Candidate.Sticky {
			return left.Candidate.Sticky
		}
		if left.Breakdown.SelectionRank != right.Breakdown.SelectionRank {
			return left.Breakdown.SelectionRank < right.Breakdown.SelectionRank
		}
		return left.Candidate.ChannelID < right.Candidate.ChannelID
	})
	return ordered
}

// ScoreRouteCandidates attaches raw observations in static priority order.
// Only OrderAutoRouteCandidates applies load buckets and weighted selection;
// latency and health samples are observations, not additive routing scores.
func ScoreRouteCandidates(candidates []RouteScoreCandidate) []ScoredRouteCandidate {
	result := make([]ScoredRouteCandidate, 0, len(candidates))
	for _, candidate := range StaticPriorityLayer(candidates) {
		result = append(result, ScoredRouteCandidate{Candidate: candidate, Breakdown: scoreRouteCandidate(candidate)})
	}
	return result
}

func scoreRouteCandidate(candidate RouteScoreCandidate) RouteScoreBreakdown {
	breakdown := RouteScoreBreakdown{
		PriorityLayer: candidate.Priority, EffectiveWeight: max(candidate.Weight, 1),
		LoadRatio: candidate.LoadRatio, LoadBucket: int(math.Floor(max(candidate.LoadRatio, 0) * 10)),
		Sticky: candidate.Sticky,
	}
	if candidate.ErrorRateKnown {
		breakdown.ErrorRate = &candidate.ErrorRate
	}
	if candidate.LatencyKnown {
		breakdown.LatencyMS = &candidate.LatencyMS
	}
	if candidate.TTFTKnown {
		breakdown.TTFTMS = &candidate.TTFTMS
	}
	if candidate.RateLimitKnown {
		breakdown.RateLimitHeadroom = &candidate.RateLimitHeadroom
	}
	if candidate.QuotaKnown {
		breakdown.QuotaHeadroom = &candidate.QuotaHeadroom
	}
	return breakdown
}

// OrderManualRouteCandidates implements the user-owned ordering boundary. A
// weight can affect presentation only inside one position layer and only when
// the group explicitly enables load balancing.
func OrderManualRouteCandidates(candidates []RouteScoreCandidate, loadBalance bool) []RouteScoreCandidate {
	ordered := append([]RouteScoreCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		if loadBalance && ordered[i].Weight != ordered[j].Weight {
			return ordered[i].Weight > ordered[j].Weight
		}
		return ordered[i].ChannelID < ordered[j].ChannelID
	})
	return ordered
}

func StaticPriorityLayer(candidates []RouteScoreCandidate) []RouteScoreCandidate {
	result := make([]RouteScoreCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.HealthUsable {
			result = append(result, candidate)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].ChannelID < result[j].ChannelID
	})
	return result
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
