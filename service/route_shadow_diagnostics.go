package service

import (
	"context"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

type RouteShadowDurableMetricsSnapshot struct {
	WindowComplete         bool    `json:"window_complete"`
	DataLossPossible       bool    `json:"data_loss_possible"`
	ShadowDecisions        int64   `json:"shadow_decisions"`
	ShadowInitialDecisions int64   `json:"shadow_initial_decisions"`
	ShadowDiffs            int64   `json:"shadow_diffs"`
	CapabilityResolved     int64   `json:"capability_resolved"`
	CapabilityUnresolved   int64   `json:"capability_unresolved"`
	MappingConflict        int64   `json:"mapping_conflict"`
	UnknownFiltered        int64   `json:"unknown_filtered"`
	UnknownAdmitted        int64   `json:"unknown_admitted"`
	MixedDecisions         int64   `json:"mixed_decisions"`
	UnauthorizedFiltered   int64   `json:"unauthorized_filtered"`
	UnauthorizedAdmitted   int64   `json:"unauthorized_admitted"`
	SnapshotStale          int64   `json:"snapshot_stale"`
	EventAttempted         int64   `json:"event_attempted"`
	EventEnqueued          int64   `json:"event_enqueued"`
	EventDropped           int64   `json:"event_dropped"`
	EventEncodeFailed      int64   `json:"event_encode_failed"`
	EventSubmitted         int64   `json:"event_submitted"`
	EventWriteFailed       int64   `json:"event_write_failed"`
	EventCompleteness      float64 `json:"event_completeness"`
	EventCompletenessKnown bool    `json:"event_completeness_known"`
	RefreshSuccess         int64   `json:"refresh_success"`
	RefreshFailure         int64   `json:"refresh_failure"`
	SnapshotConflict       int64   `json:"snapshot_conflict"`
	RefreshLagP95MS        int64   `json:"refresh_lag_p95_ms"`
	RefreshLagP95Known     bool    `json:"refresh_lag_p95_known"`
	RefreshLagCount        int64   `json:"refresh_lag_count"`
}

type RouteShadowDiagnosticsSnapshot struct {
	Metrics                      RouteShadowMetricsSnapshot        `json:"metrics"`
	Durable                      RouteShadowDurableMetricsSnapshot `json:"durable"`
	WindowStart                  int64                             `json:"window_start"`
	WindowEnd                    int64                             `json:"window_end"`
	CoreModelRequestCount        int64                             `json:"core_model_request_count"`
	CoreModelShadowDecisionCount int64                             `json:"core_model_shadow_initial_decision_count"`
	CoreModelCoverage            float64                           `json:"core_model_coverage"`
	CoreModelShadowCoverage      float64                           `json:"core_model_shadow_coverage"`
	CoreModelLabResolution       float64                           `json:"core_model_lab_resolution"`
	CoreModels                   []string                          `json:"core_models"`
	CoverageDataUnavailable      bool                              `json:"coverage_data_unavailable"`
	ShadowDataUnavailable        bool                              `json:"shadow_data_unavailable"`
	ShadowDataSource             string                            `json:"shadow_data_source"`
}

type routeShadowAggregateSnapshot struct {
	Global         model.RouteShadowHourlyObservation
	Models         map[string]model.RouteShadowHourlyObservation
	WindowComplete bool
	DataLoss       bool
}

var routeShadowDiagnosticsNow = time.Now

// GetRouteShadowDiagnostics calculates the Shadow exit indicators from
// bounded Relay aggregates and sealed primary-database hourly observations.
// Process-local counters remain visible separately, but never prove a
// cross-instance or cross-restart acceptance threshold.
func GetRouteShadowDiagnostics(ctx context.Context) RouteShadowDiagnosticsSnapshot {
	now := routeShadowDiagnosticsNow().UTC()
	windowEnd := now.Truncate(time.Hour)
	windowStart := windowEnd.Add(-7 * 24 * time.Hour)
	diagnostics := RouteShadowDiagnosticsSnapshot{
		Metrics:          RouteShadowMetrics(),
		WindowStart:      windowStart.Unix(),
		WindowEnd:        windowEnd.Unix(),
		ShadowDataSource: "primary_database_hourly_aggregate",
	}
	aggregate := loadRouteShadowAggregate(ctx, windowStart, windowEnd)
	diagnostics.Durable = durableRouteShadowMetrics(aggregate)
	if !aggregate.WindowComplete || aggregate.DataLoss {
		diagnostics.ShadowDataUnavailable = true
	}

	usage, err := model.ListRelayModelUsage(ctx, windowStart, windowEnd)
	if err != nil || len(usage) == 0 {
		diagnostics.CoverageDataUnavailable = true
		return diagnostics
	}
	var total int64
	for _, item := range usage {
		total += item.RequestCount
	}
	if total <= 0 {
		diagnostics.CoverageDataUnavailable = true
		return diagnostics
	}

	threshold := float64(total) * 0.95
	var coreVolume, shadowInitial, resolved, unresolved, conflicts int64
	for _, item := range usage {
		if float64(coreVolume) >= threshold {
			break
		}
		coreVolume += item.RequestCount
		diagnostics.CoreModels = append(diagnostics.CoreModels, item.ModelName)
		stats := aggregate.Models[modellab.NormalizeModel(item.ModelName)]
		if stats.DataLossPossible || stats.SealedAt == 0 {
			diagnostics.ShadowDataUnavailable = true
		}
		shadowInitial += stats.ShadowInitialDecisions
		resolved += stats.CapabilityResolved
		unresolved += stats.CapabilityUnresolved
		conflicts += stats.MappingConflict
	}
	if len(diagnostics.CoreModels) > 1 {
		sort.Strings(diagnostics.CoreModels)
	}
	diagnostics.CoreModelRequestCount = coreVolume
	diagnostics.CoreModelShadowDecisionCount = shadowInitial
	diagnostics.CoreModelCoverage = float64(coreVolume) / float64(total)
	diagnostics.CoreModelShadowCoverage = float64(shadowInitial) / float64(coreVolume)
	if denominator := resolved + unresolved + conflicts; denominator > 0 {
		diagnostics.CoreModelLabResolution = float64(resolved) / float64(denominator)
	} else {
		diagnostics.ShadowDataUnavailable = true
	}
	return diagnostics
}

func loadRouteShadowAggregate(ctx context.Context, windowStart, windowEnd time.Time) routeShadowAggregateSnapshot {
	result := routeShadowAggregateSnapshot{Models: make(map[string]model.RouteShadowHourlyObservation), WindowComplete: true}
	observations, err := model.ListRouteShadowHourlyObservations(ctx, windowStart.Unix(), windowEnd.Unix())
	if err != nil {
		result.WindowComplete = false
		return result
	}
	globalsByHour := make(map[int64][]model.RouteShadowHourlyObservation)
	for _, observation := range observations {
		switch observation.Scope {
		case model.RouteShadowObservationGlobal:
			globalsByHour[observation.HourStart] = append(globalsByHour[observation.HourStart], observation)
			addRouteShadowObservation(&result.Global, observation)
		case model.RouteShadowObservationModel:
			stats := result.Models[observation.ModelName]
			addRouteShadowObservation(&stats, observation)
			if observation.SealedAt == 0 {
				stats.DataLossPossible = true
			}
			result.Models[observation.ModelName] = stats
		}
	}
	for hour := windowStart.Unix(); hour < windowEnd.Unix(); hour += int64(time.Hour / time.Second) {
		rows := globalsByHour[hour]
		if len(rows) == 0 {
			result.WindowComplete = false
			continue
		}
		for _, row := range rows {
			if row.SealedAt == 0 {
				result.WindowComplete = false
			}
			if row.DataLossPossible {
				result.DataLoss = true
			}
		}
	}
	for modelName, stats := range result.Models {
		if stats.DataLossPossible {
			result.DataLoss = true
		}
		result.Models[modelName] = stats
	}
	return result
}

func durableRouteShadowMetrics(aggregate routeShadowAggregateSnapshot) RouteShadowDurableMetricsSnapshot {
	global := aggregate.Global
	metrics := RouteShadowDurableMetricsSnapshot{
		WindowComplete:         aggregate.WindowComplete,
		DataLossPossible:       aggregate.DataLoss,
		ShadowDecisions:        global.ShadowDecisions,
		ShadowInitialDecisions: global.ShadowInitialDecisions,
		ShadowDiffs:            global.ShadowDiffs,
		CapabilityResolved:     global.CapabilityResolved,
		CapabilityUnresolved:   global.CapabilityUnresolved,
		MappingConflict:        global.MappingConflict,
		UnknownFiltered:        global.UnknownFiltered,
		UnknownAdmitted:        global.UnknownAdmitted,
		MixedDecisions:         global.MixedDecisions,
		UnauthorizedFiltered:   global.UnauthorizedFiltered,
		UnauthorizedAdmitted:   global.UnauthorizedAdmitted,
		SnapshotStale:          global.SnapshotStale,
		EventAttempted:         global.EventAttempted,
		EventEnqueued:          global.EventEnqueued,
		EventDropped:           global.EventDropped,
		EventEncodeFailed:      global.EventEncodeFailed,
		EventSubmitted:         global.EventSubmitted,
		EventWriteFailed:       global.EventWriteFailed,
		RefreshSuccess:         global.RefreshSuccess,
		RefreshFailure:         global.RefreshFailure,
		SnapshotConflict:       global.SnapshotConflict,
		RefreshLagCount:        global.RefreshLagCount,
	}
	if denominator := global.EventAttempted - global.EventEncodeFailed; denominator > 0 {
		metrics.EventCompletenessKnown = true
		metrics.EventCompleteness = float64(global.EventSubmitted) / float64(denominator)
	}
	if global.RefreshLagCount > 0 {
		metrics.RefreshLagP95Known = true
		metrics.RefreshLagP95MS = routeShadowRefreshLagP95(global)
	}
	return metrics
}

func addRouteShadowObservation(target *model.RouteShadowHourlyObservation, delta model.RouteShadowHourlyObservation) {
	if target == nil {
		return
	}
	target.SealedAt = maxRouteShadowObservationValue(target.SealedAt, delta.SealedAt)
	target.DataLossPossible = target.DataLossPossible || delta.DataLossPossible
	target.ShadowDecisions += delta.ShadowDecisions
	target.ShadowInitialDecisions += delta.ShadowInitialDecisions
	target.ShadowDiffs += delta.ShadowDiffs
	target.CapabilityResolved += delta.CapabilityResolved
	target.CapabilityUnresolved += delta.CapabilityUnresolved
	target.MappingConflict += delta.MappingConflict
	target.UnknownFiltered += delta.UnknownFiltered
	target.UnknownAdmitted += delta.UnknownAdmitted
	target.MixedDecisions += delta.MixedDecisions
	target.UnauthorizedFiltered += delta.UnauthorizedFiltered
	target.UnauthorizedAdmitted += delta.UnauthorizedAdmitted
	target.SnapshotStale += delta.SnapshotStale
	target.EventAttempted += delta.EventAttempted
	target.EventEnqueued += delta.EventEnqueued
	target.EventDropped += delta.EventDropped
	target.EventEncodeFailed += delta.EventEncodeFailed
	target.EventSubmitted += delta.EventSubmitted
	target.EventWriteFailed += delta.EventWriteFailed
	target.RefreshSuccess += delta.RefreshSuccess
	target.RefreshFailure += delta.RefreshFailure
	target.SnapshotConflict += delta.SnapshotConflict
	target.RefreshLagCount += delta.RefreshLagCount
	target.RefreshLagLE1S += delta.RefreshLagLE1S
	target.RefreshLagLE5S += delta.RefreshLagLE5S
	target.RefreshLagLE15S += delta.RefreshLagLE15S
	target.RefreshLagLE30S += delta.RefreshLagLE30S
	target.RefreshLagLE60S += delta.RefreshLagLE60S
	target.RefreshLagLE120S += delta.RefreshLagLE120S
	target.RefreshLagLE300S += delta.RefreshLagLE300S
	target.RefreshLagGT300S += delta.RefreshLagGT300S
}

func routeShadowRefreshLagP95(observation model.RouteShadowHourlyObservation) int64 {
	target := (observation.RefreshLagCount*95 + 99) / 100
	var cumulative int64
	for _, bucket := range []struct {
		count int64
		upper int64
	}{
		{observation.RefreshLagLE1S, 1_000},
		{observation.RefreshLagLE5S, 5_000},
		{observation.RefreshLagLE15S, 15_000},
		{observation.RefreshLagLE30S, 30_000},
		{observation.RefreshLagLE60S, 60_000},
		{observation.RefreshLagLE120S, 120_000},
		{observation.RefreshLagLE300S, 300_000},
		{observation.RefreshLagGT300S, 300_001},
	} {
		cumulative += bucket.count
		if cumulative >= target {
			return bucket.upper
		}
	}
	return 300_001
}

func maxRouteShadowObservationValue(left, right int64) int64 {
	if right > left {
		return right
	}
	return left
}
