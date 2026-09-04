package service

import (
	"context"
	"time"
)

const (
	RouteScoreShadowSameChannel        = "same_channel"
	RouteScoreShadowDifferentSameLayer = "different_within_priority"
	RouteScoreShadowHealthDifference   = "health_filter_difference"
	RouteScoreShadowNoCandidate        = "no_scored_candidate"
	RouteScoreShadowMetricsUnavailable = "health_metrics_unavailable"
)

// AttachRouteScoreShadow adds dynamic-score diagnostics to an existing Lab
// Shadow decision. It never changes ShadowPreferredID, candidate eligibility,
// request context, billing, or retry state.
func AttachRouteScoreShadow(ctx context.Context, decision *RouteShadowDecision) {
	if decision == nil || !RouteScoreShadowEnabled() {
		return
	}
	decision.ScoreShadowEnabled = true
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	scoreCandidates := make([]RouteScoreCandidate, 0, len(decision.ShadowCandidates))
	for index := range decision.ShadowCandidates {
		candidate := &decision.ShadowCandidates[index]
		if candidate.FilterReason != "" {
			continue
		}
		health, err := LoadRouteHealth(ctx, candidate.ChannelID, decision.NormalizedRequestModel)
		if err != nil {
			decision.ScoreShadowError = RouteScoreShadowMetricsUnavailable
			return
		}
		metrics := routeHealthScoreMetrics(health)
		runtimeMetrics, metricsErr := LoadRouteScoreRuntimeMetrics(ctx, candidate.ChannelID, decision.NormalizedRequestModel)
		if metricsErr != nil {
			decision.ScoreShadowError = RouteScoreShadowMetricsUnavailable
			decision.ScoreMetricsUnavailable = true
			return
		}
		scoreCandidates = append(scoreCandidates, RouteScoreCandidate{
			ChannelID:         candidate.ChannelID,
			Priority:          candidate.Priority,
			Weight:            candidate.Weight,
			ErrorRate:         metrics.ErrorRate,
			ErrorRateKnown:    metrics.ErrorRateKnown,
			LatencyMS:         metrics.LatencyMS,
			LatencyKnown:      metrics.LatencyKnown,
			TTFTMS:            metrics.TTFTMS,
			TTFTKnown:         metrics.TTFTKnown,
			RateLimitHeadroom: runtimeMetrics.RateLimitHeadroom,
			RateLimitKnown:    runtimeMetrics.RateLimitKnown,
			QuotaHeadroom:     runtimeMetrics.QuotaHeadroom,
			QuotaKnown:        runtimeMetrics.QuotaKnown,
			Sticky:            decision.LegacyTrace.AffinityHit && candidate.ChannelID == decision.LegacyChannelID,
			HealthUsable:      CanUseRouteHealth(health, now),
		})
	}
	scored := ScoreRouteCandidates(scoreCandidates)
	scoresByChannel := make(map[int]RouteScoreBreakdown, len(scored))
	for _, candidate := range scored {
		scoresByChannel[candidate.Candidate.ChannelID] = candidate.Breakdown
	}
	for index := range decision.ShadowCandidates {
		if score, exists := scoresByChannel[decision.ShadowCandidates[index].ChannelID]; exists {
			scoreCopy := score
			decision.ShadowCandidates[index].Score = &scoreCopy
		}
	}
	if len(scored) == 0 {
		decision.ScoreShadowDifference = RouteScoreShadowNoCandidate
		return
	}
	decision.ScoreShadowPreferredID = scored[0].Candidate.ChannelID
	if decision.ScoreShadowPreferredID == decision.ShadowPreferredID {
		decision.ScoreShadowDifference = RouteScoreShadowSameChannel
		return
	}
	if shadowCandidatePriority(decision.ShadowCandidates, decision.ScoreShadowPreferredID) !=
		shadowCandidatePriority(decision.ShadowCandidates, decision.ShadowPreferredID) {
		decision.ScoreShadowDifference = RouteScoreShadowHealthDifference
		return
	}
	decision.ScoreShadowDifference = RouteScoreShadowDifferentSameLayer
}

func shadowCandidatePriority(candidates []RouteShadowCandidate, channelID int) int64 {
	for _, candidate := range candidates {
		if candidate.ChannelID == channelID {
			return candidate.Priority
		}
	}
	return 0
}
