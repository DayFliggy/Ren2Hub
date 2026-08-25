package service

import "sort"

type RouteScoreCandidate struct {
	ChannelID         int
	Priority          int64
	Position          int
	Weight            int
	ErrorRate         float64
	LatencyMS         float64
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
	priority := candidates[0].Priority
	for _, candidate := range candidates[1:] {
		if candidate.Priority > priority {
			priority = candidate.Priority
		}
	}
	result := make([]ScoredRouteCandidate, 0, len(candidates))
	maxWeight := 0
	for _, candidate := range candidates {
		if candidate.Priority == priority && candidate.Weight > maxWeight {
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
			RateLimitScore: clamp01(candidate.RateLimitHeadroom),
			QuotaScore:     clamp01(candidate.QuotaHeadroom),
		}
		if candidate.Sticky {
			breakdown.StickyScore = 1
		}
		breakdown.Total = breakdown.WeightScore + breakdown.ErrorScore + breakdown.LatencyScore + breakdown.RateLimitScore + breakdown.QuotaScore + breakdown.StickyScore
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
