package service

import (
	"errors"
	"sort"
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
	RequestModel      string
	ActualModel       string
	LabSlug           string
	Priority          int64
	Position          int
	Weight            int
	FilterReason      string
	HealthUsable      bool
	ErrorRate         float64
	LatencyMS         float64
	RateLimitHeadroom float64
	QuotaHeadroom     float64
	Sticky            bool
	SnapshotVersion   int64
	CatalogVersion    string
}

type RouteSelectionInput struct {
	SourceInput          RouteSourceInput
	ManualGroupEnabled   bool
	ManualLoadBalance    bool
	ManualCandidates     []RouteSelectionCandidate
	AutoCandidates       []RouteSelectionCandidate
	TopK                 int
	ConfigurationVersion int64
	RequestID            string
	RequestModel         string
}

type RouteSelectionResult struct {
	Decision   RouteDecision
	Candidates []RouteSelectionCandidate
}

// SelectTokenRoute applies the route-source precedence once and keeps the
// route-specific ordering rules behind one boundary. It is side-effect free:
// lease acquisition, runtime recheck, upstream setup, and billing happen in
// later stages.
func SelectTokenRoute(input RouteSelectionInput) (RouteSelectionResult, error) {
	source := ResolveRouteSource(input.SourceInput)
	decision := NewRouteDecision(input.RequestID, source, input.RequestModel, input.ConfigurationVersion)
	result := RouteSelectionResult{Decision: decision, Candidates: []RouteSelectionCandidate{}}
	if source == RouteSourceLegacy {
		return result, nil
	}

	candidates := input.AutoCandidates
	if source == RouteSourceManual {
		if !input.ManualGroupEnabled {
			return result, ErrRouteSelectionUnavailable
		}
		candidates = input.ManualCandidates
	}
	eligible := eligibleRouteSelectionCandidates(candidates)
	if len(eligible) == 0 {
		return result, ErrRouteSelectionUnavailable
	}

	if source == RouteSourceManual {
		ordered := make([]RouteScoreCandidate, 0, len(eligible))
		byID := make(map[int]RouteSelectionCandidate, len(eligible))
		for _, candidate := range eligible {
			ordered = append(ordered, RouteScoreCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				Position: candidate.Position, Weight: candidate.Weight,
				HealthUsable: candidate.HealthUsable,
			})
			byID[candidate.ChannelID] = candidate
		}
		ordered = OrderManualRouteCandidates(ordered, input.ManualLoadBalance)
		for _, item := range ordered {
			result.Candidates = append(result.Candidates, byID[item.ChannelID])
		}
	} else {
		scored := make([]RouteScoreCandidate, 0, len(eligible))
		byID := make(map[int]RouteSelectionCandidate, len(eligible))
		for _, candidate := range eligible {
			scored = append(scored, RouteScoreCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				Position: candidate.Position, Weight: candidate.Weight,
				ErrorRate: candidate.ErrorRate, LatencyMS: candidate.LatencyMS,
				RateLimitHeadroom: candidate.RateLimitHeadroom,
				QuotaHeadroom:     candidate.QuotaHeadroom,
				Sticky:            candidate.Sticky, HealthUsable: candidate.HealthUsable,
			})
			byID[candidate.ChannelID] = candidate
		}
		for _, scoredCandidate := range TopKRouteCandidates(scored, input.TopK) {
			candidate := byID[scoredCandidate.Candidate.ChannelID]
			candidateScore := scoredCandidate.Breakdown
			result.Candidates = append(result.Candidates, candidate)
			result.Decision.Candidates = append(result.Decision.Candidates, RouteDecisionCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				Position: candidate.Position, Weight: candidate.Weight,
				Score: &candidateScore, LeaseState: RouteLeaseStateNotAttempted,
			})
		}
	}

	if len(result.Candidates) == 0 {
		return RouteSelectionResult{Decision: decision}, ErrRouteSelectionUnavailable
	}
	if len(result.Decision.Candidates) == 0 {
		for _, candidate := range result.Candidates {
			result.Decision.Candidates = append(result.Decision.Candidates, RouteDecisionCandidate{
				ChannelID: candidate.ChannelID, Priority: candidate.Priority,
				Position: candidate.Position, Weight: candidate.Weight,
				LeaseState: RouteLeaseStateNotAttempted,
			})
		}
	}
	result.Decision.SelectedChannelID = result.Candidates[0].ChannelID
	return result, nil
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
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].ChannelID < candidates[j].ChannelID
	})
}
