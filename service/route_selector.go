package service

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/pkg/execution"
)

var ErrRouteSelectionUnavailable = errors.New("no eligible route candidate")

const (
	RouteCandidateFilterHealthUnavailable = "health_unavailable"
	RouteLeaseStateNotAttempted           = "not_attempted"
)

// RouteSelectionCandidate is the transport-neutral candidate passed from
// capability discovery to the unified selector. FilterReason is produced by
// the shared static qualification boundary and is never silently discarded.
type RouteSelectionCandidate struct {
	ChannelID         int
	ChannelTPM        int
	RequestModel      string
	ActualModel       string
	LabSlug           string
	Priority          int64
	Position          int
	Weight            int
	FilterReason      string
	HealthUsable      bool
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
	SnapshotVersion   int64
	HealthEpoch       int64
	CatalogVersion    string
	CompatibilityTier int
}

type RouteSelectionInput struct {
	SourceInput           RouteSourceInput
	ManualGroupEnabled    bool
	ManualLoadBalance     bool
	ManualCandidates      []RouteSelectionCandidate
	AutoCandidates        []RouteSelectionCandidate
	TopK                  int
	ConfigurationVersion  int64
	RequestID             string
	RequestModel          string
	DynamicScoringEnabled bool
}

type RouteSelectionResult struct {
	Decision   RouteDecision
	Candidates []RouteSelectionCandidate
	// ReserveCandidates are considered only after the initial TopK is exhausted.
	// Both slices together fit the request's distinct-candidate probe budget.
	ReserveCandidates []RouteSelectionCandidate
}

// SelectTokenRoute applies the route-source precedence once and keeps the
// route-specific ordering rules behind one boundary. It is side-effect free:
// lease acquisition, runtime recheck, upstream setup, and billing happen in
// later stages.
func SelectTokenRoute(input RouteSelectionInput) (RouteSelectionResult, error) {
	source := ResolveRouteSource(input.SourceInput)
	decision := NewRouteDecision(input.RequestID, source, input.RequestModel, input.ConfigurationVersion)
	result := RouteSelectionResult{Decision: decision, Candidates: []RouteSelectionCandidate{}}
	if source == RouteSourceUnavailable {
		return result, ErrRouteSelectionUnavailable
	}

	candidates := input.AutoCandidates
	if source == RouteSourceManual {
		if !input.ManualGroupEnabled {
			return result, ErrRouteSelectionUnavailable
		}
		candidates = input.ManualCandidates
	}
	candidates = normalizeRouteSelectionCandidates(candidates)
	eligible := eligibleRouteSelectionCandidates(candidates)
	if len(eligible) == 0 {
		appendRouteDecisionCandidates(&result.Decision, candidates)
		return result, ErrRouteSelectionUnavailable
	}

	if source == RouteSourceManual {
		ordered := make([]RouteScoreCandidate, 0, len(eligible))
		byID := make(map[int]RouteSelectionCandidate, len(eligible))
		for _, candidate := range eligible {
			ordered = append(ordered, RouteScoreCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				Position: candidate.Position, Weight: candidate.Weight,
				CompatibilityTier: candidate.CompatibilityTier,
				Sticky:            candidate.Sticky,
				HealthUsable:      candidate.HealthUsable,
			})
			byID[candidate.ChannelID] = candidate
		}
		ordered = orderManualRouteCandidatesForSelection(ordered, input.ManualLoadBalance, input.RequestID)
		for _, item := range ordered {
			result.Candidates = append(result.Candidates, byID[item.ChannelID])
		}
	} else {
		staticCandidates := make([]RouteScoreCandidate, 0, len(eligible))
		byID := make(map[int]RouteSelectionCandidate, len(eligible))
		for _, candidate := range eligible {
			staticCandidates = append(staticCandidates, RouteScoreCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				Position: candidate.Position, Weight: candidate.Weight,
				CompatibilityTier: candidate.CompatibilityTier,
				HealthUsable:      candidate.HealthUsable,
			})
			byID[candidate.ChannelID] = candidate
		}
		staticCandidates = StaticPriorityLayer(staticCandidates)
		orderedCandidates := make([]ScoredRouteCandidate, 0, len(staticCandidates))
		for _, candidate := range staticCandidates {
			orderedCandidates = append(orderedCandidates, ScoredRouteCandidate{Candidate: candidate})
		}
		result.Decision.ScoringMode = "off"
		if len(staticCandidates) > 0 {
			result.Decision.StaticPreferredChannelID = staticCandidates[0].ChannelID
		}
		if input.DynamicScoringEnabled {
			scored := make([]RouteScoreCandidate, 0, len(eligible))
			for _, candidate := range eligible {
				scored = append(scored, RouteScoreCandidate{
					ChannelID: candidate.ChannelID, Priority: candidate.Priority,
					Position: candidate.Position, Weight: candidate.Weight,
					CompatibilityTier: candidate.CompatibilityTier,
					ErrorRate:         candidate.ErrorRate, ErrorRateKnown: candidate.ErrorRateKnown,
					LatencyMS: candidate.LatencyMS, LatencyKnown: candidate.LatencyKnown,
					TTFTMS: candidate.TTFTMS, TTFTKnown: candidate.TTFTKnown,
					RateLimitHeadroom: candidate.RateLimitHeadroom,
					RateLimitKnown:    candidate.RateLimitKnown,
					QuotaHeadroom:     candidate.QuotaHeadroom,
					QuotaKnown:        candidate.QuotaKnown,
					Sticky:            candidate.Sticky, HealthUsable: candidate.HealthUsable,
				})
			}
			scoredCandidates := ScoreRouteCandidates(scored)
			if len(scoredCandidates) > 0 {
				result.Decision.ScoredPreferredChannelID = scoredCandidates[0].Candidate.ChannelID
			}
			result.Decision.ScoringMode = "live"
			result.Decision.DynamicScoreApplied = true
			orderedCandidates = weightedRouteCandidateOrder(scoredCandidates, input.RequestID, false)
			appendRouteDecisionCandidatesWithScores(&result.Decision, candidates, scoredCandidates)
		} else {
			appendRouteDecisionCandidates(&result.Decision, candidates)
			orderedCandidates = weightedRouteCandidateOrder(orderedCandidates, input.RequestID, false)
		}
		limit := normalizedRouteTopK(input.TopK)
		if len(orderedCandidates) > execution.MaxCandidates {
			orderedCandidates = orderedCandidates[:execution.MaxCandidates]
		}
		for index, scoredCandidate := range orderedCandidates {
			candidate := byID[scoredCandidate.Candidate.ChannelID]
			if index < limit {
				result.Candidates = append(result.Candidates, candidate)
			} else {
				result.ReserveCandidates = append(result.ReserveCandidates, candidate)
			}
		}
	}
	if len(result.Candidates) > execution.MaxCandidates {
		result.Candidates = result.Candidates[:execution.MaxCandidates]
	}

	if len(result.Candidates) == 0 {
		appendRouteDecisionCandidates(&result.Decision, candidates)
		return result, ErrRouteSelectionUnavailable
	}
	appendRouteDecisionCandidates(&result.Decision, candidates)
	if len(result.Decision.Candidates) == 0 {
		for _, candidate := range result.Candidates {
			result.Decision.Candidates = append(result.Decision.Candidates, RouteDecisionCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				ChannelTPM:   candidate.ChannelTPM,
				RequestModel: candidate.RequestModel, ActualModel: candidate.ActualModel,
				Position: candidate.Position, Weight: candidate.Weight,
				SnapshotVersion: candidate.SnapshotVersion, CatalogVersion: candidate.CatalogVersion,
				HealthEpoch: candidate.HealthEpoch,
				LeaseState:  RouteLeaseStateNotAttempted,
			})
		}
	}
	result.Decision.SelectedChannelID = result.Candidates[0].ChannelID
	result.Decision.ActualModel = result.Candidates[0].ActualModel
	result.Decision.LabSlug = result.Candidates[0].LabSlug
	result.Decision.CatalogVersion = result.Candidates[0].CatalogVersion
	result.Decision.SnapshotVersion = result.Candidates[0].SnapshotVersion
	return result, nil
}

// orderManualRouteCandidatesForSelection keeps the user-owned position
// boundary intact. Every position layer is sampled without replacement so
// both initial selection and failover honor the same weights.
func orderManualRouteCandidatesForSelection(candidates []RouteScoreCandidate, loadBalance bool, requestID string) []RouteScoreCandidate {
	ordered := OrderManualRouteCandidates(candidates, loadBalance)
	if !loadBalance || len(ordered) < 2 || requestID == "" {
		return ordered
	}

	scored := make([]ScoredRouteCandidate, 0, len(ordered))
	for _, candidate := range ordered {
		scored = append(scored, ScoredRouteCandidate{Candidate: candidate})
	}
	for index, candidate := range weightedRouteCandidateOrder(scored, requestID, true) {
		ordered[index] = candidate.Candidate
	}
	return ordered
}

func normalizeRouteSelectionCandidates(candidates []RouteSelectionCandidate) []RouteSelectionCandidate {
	normalized := append([]RouteSelectionCandidate(nil), candidates...)
	for index := range normalized {
		if normalized[index].FilterReason == "" && !normalized[index].HealthUsable {
			normalized[index].FilterReason = RouteCandidateFilterHealthUnavailable
		}
	}
	return normalized
}

func appendRouteDecisionCandidates(decision *RouteDecision, candidates []RouteSelectionCandidate) {
	appendRouteDecisionCandidatesWithScores(decision, candidates, nil)
}

func appendRouteDecisionCandidatesWithScores(decision *RouteDecision, candidates []RouteSelectionCandidate, scores []ScoredRouteCandidate) {
	if decision == nil {
		return
	}
	scoresByChannel := make(map[int]RouteScoreBreakdown, len(scores))
	for _, scored := range scores {
		scoresByChannel[scored.Candidate.ChannelID] = scored.Breakdown
	}
	seen := make(map[int]struct{}, len(decision.Candidates))
	for _, candidate := range decision.Candidates {
		seen[candidate.ChannelID] = struct{}{}
	}
	for _, candidate := range candidates {
		if candidate.ChannelID <= 0 {
			continue
		}
		if _, exists := seen[candidate.ChannelID]; exists {
			for index := range decision.Candidates {
				if decision.Candidates[index].ChannelID == candidate.ChannelID && decision.Candidates[index].FilterReason == "" {
					decision.Candidates[index].FilterReason = candidate.FilterReason
				}
			}
			continue
		}
		seen[candidate.ChannelID] = struct{}{}
		var score *RouteScoreBreakdown
		if breakdown, exists := scoresByChannel[candidate.ChannelID]; exists {
			breakdownCopy := breakdown
			score = &breakdownCopy
		}
		decision.Candidates = append(decision.Candidates, RouteDecisionCandidate{
			ChannelID: candidate.ChannelID, FilterReason: candidate.FilterReason,
			ChannelTPM:   candidate.ChannelTPM,
			RequestModel: candidate.RequestModel, ActualModel: candidate.ActualModel,
			Priority: candidate.Priority, Position: candidate.Position, Weight: candidate.Weight,
			SnapshotVersion: candidate.SnapshotVersion, CatalogVersion: candidate.CatalogVersion,
			HealthEpoch: candidate.HealthEpoch,
			Score:       score, LeaseState: RouteLeaseStateNotAttempted,
		})
	}
}

// DefaultRouteCandidateTopK bounds how many candidates automatic routing plans
// per request. It is deliberately larger than the send budget: a candidate may
// be refused by admission (capacity, rate budget, stale qualification) without
// ever reaching an upstream, so the plan needs room to fail over while
// DefaultTotalAttempts still caps actual sends.
const DefaultRouteCandidateTopK = 7

func normalizedRouteTopK(k int) int {
	if k <= 0 || k > DefaultRouteCandidateTopK {
		return DefaultRouteCandidateTopK
	}
	return k
}

func eligibleRouteSelectionCandidates(candidates []RouteSelectionCandidate) []RouteSelectionCandidate {
	eligible := make([]RouteSelectionCandidate, 0, len(candidates))
	seen := make(map[int]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.ChannelID <= 0 || candidate.FilterReason != "" || !candidate.HealthUsable {
			continue
		}
		if _, exists := seen[candidate.ChannelID]; exists {
			continue
		}
		seen[candidate.ChannelID] = struct{}{}
		eligible = append(eligible, candidate)
	}
	return eligible
}

// SortRouteSelectionCandidates provides a stable display/debug order without
// changing the source-specific selector semantics.
func SortRouteSelectionCandidates(candidates []RouteSelectionCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].CompatibilityTier != candidates[j].CompatibilityTier {
			return candidates[i].CompatibilityTier < candidates[j].CompatibilityTier
		}
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].ChannelID < candidates[j].ChannelID
	})
}
