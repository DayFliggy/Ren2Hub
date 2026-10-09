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
	ErrorBand         RouteRankBand
	ErrorBandKnown    bool
	TTFTBand          RouteRankBand
	TTFTBandKnown     bool
}

type RouteScoreBreakdown struct {
	PriorityLayer     int64    `json:"priority_layer"`
	EffectiveWeight   int      `json:"effective_weight"`
	LoadRatio         float64  `json:"load_ratio"`
	LoadBucket        int      `json:"load_bucket"`
	Sticky            bool     `json:"sticky"`
	SelectionRank     float64  `json:"selection_rank"`
	ErrorBand         *int     `json:"error_band,omitempty"`
	TTFTBand          *int     `json:"ttft_band,omitempty"`
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

// OrderAutoRouteCandidates keeps priority as an absolute layer and then orders
// inside it by observed health, load, and time to first token before falling
// back to the weighted rendezvous permutation. A degraded channel therefore
// loses to a healthy peer at the same priority but never crosses a priority
// boundary the administrator set.
//
// Inside one tie group the weight follows NewAPI's GetRandomSatisfiedChannel:
// first attempts are proportional to the configured weight, a zero-weight
// channel gets none while a weighted peer exists, and an all-zero group shares
// equally. Zero-weight channels rank after weighted ones and use rendezvous
// weight 1 among themselves, so they remain ordered failover candidates.
func OrderAutoRouteCandidates(candidates []RouteScoreCandidate, requestID string) []ScoredRouteCandidate {
	ordered := ScoreRouteCandidates(candidates)
	for index := range ordered {
		candidate := ordered[index].Candidate
		digest := sha256.Sum256([]byte(requestID + ":" + strconv.Itoa(candidate.ChannelID)))
		uniform := (float64(binary.BigEndian.Uint64(digest[:8])>>11) + 1) / (float64(uint64(1)<<53) + 1)
		ordered[index].Breakdown.SelectionRank = -math.Log(uniform) / float64(max(ordered[index].Breakdown.EffectiveWeight, 1))
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Candidate.Priority != right.Candidate.Priority {
			return left.Candidate.Priority > right.Candidate.Priority
		}
		leftError, rightError := comparableRankBand(left.Candidate.ErrorBand, left.Candidate.ErrorBandKnown), comparableRankBand(right.Candidate.ErrorBand, right.Candidate.ErrorBandKnown)
		if leftError != rightError {
			return leftError < rightError
		}
		if left.Breakdown.LoadBucket != right.Breakdown.LoadBucket {
			return left.Breakdown.LoadBucket < right.Breakdown.LoadBucket
		}
		leftTTFT, rightTTFT := comparableRankBand(left.Candidate.TTFTBand, left.Candidate.TTFTBandKnown), comparableRankBand(right.Candidate.TTFTBand, right.Candidate.TTFTBandKnown)
		if leftTTFT != rightTTFT {
			return leftTTFT < rightTTFT
		}
		if left.Candidate.Sticky != right.Candidate.Sticky {
			return left.Candidate.Sticky
		}
		if leftWeighted, rightWeighted := left.Breakdown.EffectiveWeight > 0, right.Breakdown.EffectiveWeight > 0; leftWeighted != rightWeighted {
			return leftWeighted
		}
		if left.Breakdown.SelectionRank != right.Breakdown.SelectionRank {
			return left.Breakdown.SelectionRank < right.Breakdown.SelectionRank
		}
		return left.Candidate.ChannelID < right.Candidate.ChannelID
	})
	return ordered
}

// comparableRankBand ranks an unobserved band with good ones. A cold channel
// below the sample floor must compete on load and weight; ordering it after
// known bands would starve it of first attempts, and it could then never
// collect the samples that make its band known.
func comparableRankBand(band RouteRankBand, known bool) RouteRankBand {
	if !known {
		return RouteRankBandGood
	}
	return band
}

// ScoreRouteCandidates attaches observations in static priority order. Bands
// and the load bucket are hard comparison keys applied by
// OrderAutoRouteCandidates; latency and error rates remain observations so the
// decision record stays explainable.
func ScoreRouteCandidates(candidates []RouteScoreCandidate) []ScoredRouteCandidate {
	result := make([]ScoredRouteCandidate, 0, len(candidates))
	for _, candidate := range StaticPriorityLayer(candidates) {
		result = append(result, ScoredRouteCandidate{Candidate: candidate, Breakdown: scoreRouteCandidate(candidate)})
	}
	return result
}

func scoreRouteCandidate(candidate RouteScoreCandidate) RouteScoreBreakdown {
	breakdown := RouteScoreBreakdown{
		PriorityLayer: candidate.Priority,
		// The error and TTFT bands already order health as hard keys, so the
		// weighted step keeps the administrator's weight unchanged.
		EffectiveWeight: max(candidate.Weight, 0),
		LoadRatio:       candidate.LoadRatio, LoadBucket: int(math.Floor(max(candidate.LoadRatio, 0) * 20)),
		Sticky: candidate.Sticky,
	}
	if candidate.ErrorBandKnown {
		band := int(candidate.ErrorBand)
		breakdown.ErrorBand = &band
	}
	if candidate.TTFTBandKnown {
		band := int(candidate.TTFTBand)
		breakdown.TTFTBand = &band
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
