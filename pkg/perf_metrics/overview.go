package perfmetrics

import (
	"math"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

const (
	overviewMaxPoints = 1440
	overviewTopModels = 6
	OverviewMinHours  = 1
	OverviewMaxHours  = 24 * 7
)

type OverviewStats struct {
	Requests     int64    `json:"requests"`
	SuccessRate  *float64 `json:"success_rate"`
	AvgLatencyMs int64    `json:"avg_latency_ms"`
	AvgTps       float64  `json:"avg_tps"`
}

type OverviewPoint struct {
	Ts int64 `json:"ts"`
	OverviewStats
}

type OverviewModel struct {
	ModelName string `json:"model_name"`
	OverviewStats
}

type OverviewResult struct {
	Enabled       bool            `json:"enabled"`
	Hours         int             `json:"hours"`
	BucketSeconds int64           `json:"bucket_seconds"`
	Summary       OverviewStats   `json:"summary"`
	Series        []OverviewPoint `json:"series"`
	TopModels     []OverviewModel `json:"top_models"`
}

// QueryOverview aggregates all models into one zero-filled series. Callers
// bound hours to [OverviewMinHours, OverviewMaxHours].
func QueryOverview(hours int) (OverviewResult, error) {
	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600
	step := perf_metrics_setting.GetBucketSeconds()
	if step <= 0 {
		step = 3600
	}
	for (endTs-startTs)/step+1 > overviewMaxPoints {
		step *= 2
	}
	// Start at a bucket boundary so the first point is a whole bucket, not a
	// partial one that would read as a traffic dip.
	first := startTs - startTs%step
	totals, modelBuckets, err := collectModelBuckets(first, endTs, nil)
	if err != nil {
		return OverviewResult{}, err
	}

	merged := map[int64]counters{}
	for _, buckets := range modelBuckets {
		for bucketTs, value := range buckets {
			aligned := bucketTs - bucketTs%step
			current := merged[aligned]
			current.add(value)
			merged[aligned] = current
		}
	}
	series := make([]OverviewPoint, 0, (endTs-first)/step+1)
	var all counters
	for ts := first; ts <= endTs; ts += step {
		all.add(merged[ts])
		series = append(series, OverviewPoint{Ts: ts, OverviewStats: overviewStats(merged[ts])})
	}

	models := make([]OverviewModel, 0, len(totals))
	for name, total := range totals {
		if total.requestCount > 0 {
			models = append(models, OverviewModel{ModelName: name, OverviewStats: overviewStats(total)})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Requests != models[j].Requests {
			return models[i].Requests > models[j].Requests
		}
		return models[i].ModelName < models[j].ModelName
	})
	if len(models) > overviewTopModels {
		models = models[:overviewTopModels]
	}

	return OverviewResult{
		Enabled:       perf_metrics_setting.GetSetting().Enabled,
		Hours:         hours,
		BucketSeconds: step,
		Summary:       overviewStats(all),
		Series:        series,
		TopModels:     models,
	}, nil
}

// An empty bucket has no success rate; zero would read as a total outage.
func overviewStats(value counters) OverviewStats {
	stats := OverviewStats{
		Requests:     value.requestCount,
		AvgLatencyMs: avg(value.totalLatencyMs, value.requestCount),
		AvgTps:       math.Round(avgTps(value)*100) / 100,
	}
	if value.requestCount > 0 {
		rate := math.Round(successRate(value)*100) / 100
		stats.SuccessRate = &rate
	}
	return stats
}
