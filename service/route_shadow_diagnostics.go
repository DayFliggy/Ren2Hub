package service

import (
	"context"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

type RouteShadowDiagnosticsSnapshot struct {
	Metrics                 RouteShadowMetricsSnapshot `json:"metrics"`
	WindowStart             int64                      `json:"window_start"`
	CoreModelRequestCount   int64                      `json:"core_model_request_count"`
	CoreModelCoverage       float64                    `json:"core_model_coverage"`
	CoreModelLabResolution  float64                    `json:"core_model_lab_resolution"`
	CoreModels              []string                   `json:"core_models"`
	CoverageDataUnavailable bool                       `json:"coverage_data_unavailable"`
	ShadowDataUnavailable   bool                       `json:"shadow_data_unavailable"`
	ShadowDataSource        string                     `json:"shadow_data_source"`
}

// RouteShadowDiagnostics calculates the PR-3A exit indicators from aggregate
// logs and in-memory Shadow observations. It is intended for the existing
// administrator test-status endpoint, never for request selection.
func GetRouteShadowDiagnostics(ctx context.Context) RouteShadowDiagnosticsSnapshot {
	windowStart := time.Now().Add(-7 * 24 * time.Hour)
	diagnostics := RouteShadowDiagnosticsSnapshot{
		Metrics:     RouteShadowMetrics(),
		WindowStart: windowStart.Unix(),
	}
	usage, err := model.ListRecentRelayModelUsage(ctx, windowStart)
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
	var coreVolume int64
	aggregate := loadRouteShadowAggregate(ctx, windowStart)
	diagnostics.ShadowDataSource = "redis_hourly_aggregate"
	if !aggregate.Available {
		diagnostics.ShadowDataUnavailable = true
	}
	var resolved, decisions uint64
	for _, item := range usage {
		if float64(coreVolume) >= threshold {
			break
		}
		coreVolume += item.RequestCount
		diagnostics.CoreModels = append(diagnostics.CoreModels, item.ModelName)
		stats := aggregate.Models[modellab.NormalizeModel(item.ModelName)]
		if stats.Decisions == 0 {
			diagnostics.ShadowDataUnavailable = true
			continue
		}
		decisions += stats.Decisions
		resolved += stats.Resolved
	}
	if len(diagnostics.CoreModels) > 1 {
		sort.Strings(diagnostics.CoreModels)
	}
	diagnostics.CoreModelRequestCount = coreVolume
	diagnostics.CoreModelCoverage = float64(coreVolume) / float64(total)
	if !diagnostics.ShadowDataUnavailable && decisions > 0 {
		diagnostics.CoreModelLabResolution = float64(resolved) / float64(decisions)
	}
	return diagnostics
}
