package service

import "sort"

type RouteScoreCandidate struct {
	ChannelID         int
	Priority          int64
	Position          int
	Weight            int
	ErrorRate         float64
	LatencyMS         float64
	TTFTMS            float64
	RateLimitHeadroom float64
	QuotaHeadroom     float64
	Sticky            bool
	HealthUsable      bool
}

type RouteScoreBreakdown struct {
	PriorityLayer  int64
	WeightScore    float64
	ErrorScore     float64
	LatencyScore   float64
	TTFTScore      float64
	RateLimitScore float64
	QuotaScore     float64
	StickyScore    float64
	Total          float64
}

type ScoredRouteCandidate struct {
	Candidate RouteScoreCandidate
	Breakdown RouteScoreBreakdown
}

// ScoreRouteCandidates first fences candidates by the Ren2Hub priority
// direction (larger value wins), then scores only the top static layer. This
// prevents health or latency from silently overriding administrator priority.
func ScoreRouteCandidates(candidates []RouteScoreCandidate) []ScoredRouteCandidate {
	if len(candidates) == 0 {
		return []ScoredRouteCandidate{}
	}
	var priority int64
	foundHealthyCandidate := false
	for _, candidate := range candidates {
		if !candidate.HealthUsable {
			continue
		}
		if !foundHealthyCandidate || candidate.Priority > priority {
			priority = candidate.Priority
		}
		foundHealthyCandidate = true
	}
	if !foundHealthyCandidate {
		return []ScoredRouteCandidate{}
	}
	result := make([]ScoredRouteCandidate, 0, len(candidates))
	maxWeight := 0
	for _, candidate := range candidates {
		if candidate.Priority == priority && candidate.HealthUsable && candidate.Weight > maxWeight {
			maxWeight = candidate.Weight
		}
	}
	if maxWeight <= 0 {
		maxWeight = 1
	}
	for _, candidate := range candidates {
		if candidate.Priority != priority || !candidate.HealthUsable {
			continue
		}
		breakdown := RouteScoreBreakdown{
			PriorityLayer:  candidate.Priority,
			WeightScore:    float64(maxInt(candidate.Weight, 0)) / float64(maxWeight),
			ErrorScore:     clamp01(1 - candidate.ErrorRate),
			LatencyScore:   inverseLatencyScore(candidate.LatencyMS),
			TTFTScore:      inverseLatencyScore(candidate.TTFTMS),
			RateLimitScore: clamp01(candidate.RateLimitHeadroom),
			QuotaScore:     clamp01(candidate.QuotaHeadroom),
		}
		if candidate.Sticky {
			breakdown.StickyScore = 1
		}
		breakdown.Total = breakdown.WeightScore + breakdown.ErrorScore + breakdown.LatencyScore + breakdown.TTFTScore + breakdown.RateLimitScore + breakdown.QuotaScore + breakdown.StickyScore
		result = append(result, ScoredRouteCandidate{Candidate: candidate, Breakdown: breakdown})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Breakdown.Total != result[j].Breakdown.Total {
			return result[i].Breakdown.Total > result[j].Breakdown.Total
		}
		return result[i].Candidate.ChannelID < result[j].Candidate.ChannelID
	})
	return result
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
	scored := ScoreRouteCandidates(candidates)
	result := make([]RouteScoreCandidate, 0, len(scored))
	for _, candidate := range scored {
		result = append(result, candidate.Candidate)
	}
	return result
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
