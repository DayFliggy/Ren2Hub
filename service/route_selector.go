package service

import (
	"errors"
	"hash/fnv"
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
				HealthUsable: candidate.HealthUsable,
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
				HealthUsable: candidate.HealthUsable,
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
		if input.DynamicScoringEnabled || RouteScoreShadowEnabled() {
			scored := make([]RouteScoreCandidate, 0, len(eligible))
			for _, candidate := range eligible {
				scored = append(scored, RouteScoreCandidate{
					ChannelID: candidate.ChannelID, Priority: candidate.Priority,
					Position: candidate.Position, Weight: candidate.Weight,
					ErrorRate: candidate.ErrorRate, ErrorRateKnown: candidate.ErrorRateKnown,
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
			if input.DynamicScoringEnabled {
				result.Decision.ScoringMode = "live"
				result.Decision.DynamicScoreApplied = true
				orderedCandidates = scoredCandidates
			} else {
				result.Decision.ScoringMode = "shadow"
				orderedCandidates = scoredCandidatesInStaticOrder(staticCandidates, scoredCandidates)
			}
			appendRouteDecisionCandidatesWithScores(&result.Decision, candidates, scoredCandidates)
		} else {
			appendRouteDecisionCandidates(&result.Decision, candidates)
		}
		limit := normalizedRouteTopK(input.TopK)
		if len(orderedCandidates) > limit {
			orderedCandidates = orderedCandidates[:limit]
		}
		for _, scoredCandidate := range orderedCandidates {
			candidate := byID[scoredCandidate.Candidate.ChannelID]
			result.Candidates = append(result.Candidates, candidate)
		}
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
// boundary intact. When load balancing is enabled, only the first eligible
// position layer uses a request-stable weighted choice for the first attempt;
// all later candidates retain a deterministic order for bounded failover.
func orderManualRouteCandidatesForSelection(candidates []RouteScoreCandidate, loadBalance bool, requestID string) []RouteScoreCandidate {
	ordered := OrderManualRouteCandidates(candidates, loadBalance)
	if !loadBalance || len(ordered) < 2 || requestID == "" {
		return ordered
	}

	firstLayerEnd := 1
	for firstLayerEnd < len(ordered) && ordered[firstLayerEnd].Position == ordered[0].Position {
		firstLayerEnd++
	}
	if firstLayerEnd < 2 {
		return ordered
	}

	selectedIndex := weightedManualCandidateIndex(ordered[:firstLayerEnd], requestID)
	if selectedIndex == 0 {
		return ordered
	}
	selected := ordered[selectedIndex]
	copy(ordered[1:selectedIndex+1], ordered[:selectedIndex])
	ordered[0] = selected
	return ordered
}

// weightedManualCandidateIndex chooses from one manual position layer without
// introducing process-local randomness. Request IDs are already unique at the
// relay boundary, so the FNV bucket yields repeatable decisions for a given
// request while distributing independent requests according to their weights.
func weightedManualCandidateIndex(candidates []RouteScoreCandidate, requestID string) int {
	if len(candidates) < 2 || requestID == "" {
		return 0
	}

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(requestID))
	var total uint64
	for _, candidate := range candidates {
		if candidate.Weight > 0 {
			total += uint64(candidate.Weight)
		}
	}
	if total == 0 {
		return int(hasher.Sum64() % uint64(len(candidates)))
	}

	bucket := hasher.Sum64() % total
	var accumulated uint64
	for index, candidate := range candidates {
		if candidate.Weight <= 0 {
			continue
		}
		accumulated += uint64(candidate.Weight)
		if bucket < accumulated {
			return index
		}
	}
	return 0
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
			Priority: candidate.Priority, Position: candidate.Position, Weight: candidate.Weight,
			SnapshotVersion: candidate.SnapshotVersion, CatalogVersion: candidate.CatalogVersion,
			HealthEpoch: candidate.HealthEpoch,
			Score:       score, LeaseState: RouteLeaseStateNotAttempted,
		})
	}
}

func scoredCandidatesInStaticOrder(static []RouteScoreCandidate, scored []ScoredRouteCandidate) []ScoredRouteCandidate {
	byID := make(map[int]ScoredRouteCandidate, len(scored))
	for _, candidate := range scored {
		byID[candidate.Candidate.ChannelID] = candidate
	}
	ordered := make([]ScoredRouteCandidate, 0, len(scored))
	for _, candidate := range static {
		if scoredCandidate, exists := byID[candidate.ChannelID]; exists {
			ordered = append(ordered, scoredCandidate)
		}
	}
	return ordered
}

func normalizedRouteTopK(k int) int {
	if k <= 0 || k > 3 {
		return 3
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
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].ChannelID < candidates[j].ChannelID
	})
}
