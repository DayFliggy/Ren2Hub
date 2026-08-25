package service

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
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
}

type RouteScoreBreakdown struct {
	PriorityLayer       int64   `json:"priority_layer"`
	WeightScore         float64 `json:"weight_score"`
	ErrorScore          float64 `json:"error_score"`
	ErrorKnown          bool    `json:"error_known"`
	LatencyScore        float64 `json:"latency_score"`
	LatencyKnown        bool    `json:"latency_known"`
	TTFTScore           float64 `json:"ttft_score"`
	TTFTKnown           bool    `json:"ttft_known"`
	RateLimitScore      float64 `json:"rate_limit_score"`
	RateLimitKnown      bool    `json:"rate_limit_known"`
	QuotaScore          float64 `json:"quota_score"`
	QuotaKnown          bool    `json:"quota_known"`
	CircuitBreakerScore float64 `json:"circuit_breaker_score"`
	StickyScore         float64 `json:"sticky_score"`
	Total               float64 `json:"total"`
}

type ScoredRouteCandidate struct {
	Candidate RouteScoreCandidate
	Breakdown RouteScoreBreakdown
}

const unknownRouteMetricScore = 0.5

// ScoreRouteCandidates preserves every healthy static layer in descending
// Ren2Hub priority order and applies dynamic scoring only inside each layer.
// Lower layers therefore remain available for bounded failover, while no
// dynamic metric can overtake an administrator-defined priority boundary.
func ScoreRouteCandidates(candidates []RouteScoreCandidate) []ScoredRouteCandidate {
	if len(candidates) == 0 {
		return []ScoredRouteCandidate{}
	}
	layers := make(map[int64][]RouteScoreCandidate)
	priorities := make([]int64, 0)
	for _, candidate := range candidates {
		if !candidate.HealthUsable {
			continue
		}
		if _, exists := layers[candidate.Priority]; !exists {
			priorities = append(priorities, candidate.Priority)
		}
		layers[candidate.Priority] = append(layers[candidate.Priority], candidate)
	}
	if len(priorities) == 0 {
		return []ScoredRouteCandidate{}
	}
	sort.Slice(priorities, func(i, j int) bool { return priorities[i] > priorities[j] })
	result := make([]ScoredRouteCandidate, 0, len(candidates))
	for _, priority := range priorities {
		layer := layers[priority]
		maxWeight := 0
		for _, candidate := range layer {
			if candidate.Weight > maxWeight {
				maxWeight = candidate.Weight
			}
		}
		if maxWeight <= 0 {
			maxWeight = 1
		}
		for _, candidate := range layer {
			breakdown := scoreRouteCandidate(candidate, maxWeight)
			result = append(result, ScoredRouteCandidate{Candidate: candidate, Breakdown: breakdown})
		}
		start := len(result) - len(layer)
		sort.SliceStable(result[start:], func(i, j int) bool {
			left := result[start+i]
			right := result[start+j]
			if left.Breakdown.Total != right.Breakdown.Total {
				return left.Breakdown.Total > right.Breakdown.Total
			}
			return left.Candidate.ChannelID < right.Candidate.ChannelID
		})
	}
	return result
}

func scoreRouteCandidate(candidate RouteScoreCandidate, maxWeight int) RouteScoreBreakdown {
	breakdown := RouteScoreBreakdown{
		PriorityLayer:       candidate.Priority,
		WeightScore:         float64(maxInt(candidate.Weight, 0)) / float64(maxWeight),
		ErrorScore:          unknownRouteMetricScore,
		LatencyScore:        unknownRouteMetricScore,
		TTFTScore:           unknownRouteMetricScore,
		RateLimitScore:      unknownRouteMetricScore,
		QuotaScore:          unknownRouteMetricScore,
		CircuitBreakerScore: 1,
	}
	if candidate.ErrorRateKnown {
		breakdown.ErrorKnown = true
		breakdown.ErrorScore = clamp01(1 - candidate.ErrorRate)
	}
	if candidate.LatencyKnown {
		breakdown.LatencyKnown = true
		breakdown.LatencyScore = inverseLatencyScore(candidate.LatencyMS)
	}
	if candidate.TTFTKnown {
		breakdown.TTFTKnown = true
		breakdown.TTFTScore = inverseLatencyScore(candidate.TTFTMS)
	}
	if candidate.RateLimitKnown {
		breakdown.RateLimitKnown = true
		breakdown.RateLimitScore = clamp01(candidate.RateLimitHeadroom)
	}
	if candidate.QuotaKnown {
		breakdown.QuotaKnown = true
		breakdown.QuotaScore = clamp01(candidate.QuotaHeadroom)
	}
	if candidate.Sticky {
		breakdown.StickyScore = 1
	}
	breakdown.Total = breakdown.WeightScore + breakdown.ErrorScore + breakdown.LatencyScore + breakdown.TTFTScore +
		breakdown.RateLimitScore + breakdown.QuotaScore + breakdown.CircuitBreakerScore + breakdown.StickyScore
	return breakdown
}

// TopKRouteCandidates applies the bounded candidate budget used by the future
// live selector. A caller may request fewer than three candidates, but never
// more than the initial safety limit.
func TopKRouteCandidates(candidates []RouteScoreCandidate, k int) []ScoredRouteCandidate {
	if k <= 0 || k > 3 {
		k = 3
	}
	scored := ScoreRouteCandidates(candidates)
	if len(scored) > k {
		return scored[:k]
	}
	return scored
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

func RouteScoreShadowEnabled() bool {
	return common.GetEnvOrDefaultBool("ROUTE_SCORE_SHADOW_ENABLED", false)
}

func RouteScoreLiveEnabled() bool {
	return RouteScoreShadowEnabled() && common.GetEnvOrDefaultBool("ROUTE_SCORE_LIVE_ENABLED", false)
}

func inverseLatencyScore(latency float64) float64 {
	if latency <= 0 {
		return 1
	}
	return clamp01(1000 / (1000 + latency))
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

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
