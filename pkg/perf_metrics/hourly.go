package perfmetrics

import (
	"math"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

// HourlyPoint 是跨模型合计的一个小时。没有请求的小时各比率为 null，不伪造 0%。
type HourlyPoint struct {
	Ts           int64    `json:"ts"`
	RequestCount int64    `json:"request_count"`
	SuccessCount int64    `json:"success_count"`
	SuccessRate  *float64 `json:"success_rate"`
	AvgLatencyMs *int64   `json:"avg_latency_ms"`
	AvgTps       *float64 `json:"avg_tps"`
}

// HourlyResult 供运维质量卡使用：窗口合计复用原生 Summary，序列按小时升序且不缺小时。
type HourlyResult struct {
	Enabled       bool          `json:"enabled"`
	WindowStart   int64         `json:"window_start"`
	WindowEnd     int64         `json:"window_end"`
	BucketSeconds int64         `json:"bucket_seconds"`
	Summary       *Summary      `json:"summary"`
	Series        []HourlyPoint `json:"series"`
}

// QueryHourlyAll 按小时跨模型汇总 perf_metrics，并合入尚未落库的热桶，
// 窗口与原生 summary 相同（当前不完整小时加之前的整小时）。
func QueryHourlyAll(hours int) (HourlyResult, error) {
	startTs, endTs := queryWindow(time.Now(), hours)
	rows, err := model.GetPerfMetricsSummaryBucketsAll(startTs, endTs)
	if err != nil {
		return HourlyResult{}, err
	}
	hourly := map[int64]counters{}
	for _, row := range rows {
		addHourly(hourly, row.BucketTs, counters{
			requestCount: row.RequestCount, successCount: row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs, outputTokens: row.OutputTokens, generationMs: row.GenerationMs,
		})
	}
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if k.bucketTs >= startTs && k.bucketTs <= endTs {
			addHourly(hourly, k.bucketTs, value.(*atomicBucket).snapshot())
		}
		return true
	})
	result := HourlyResult{
		Enabled: perf_metrics_setting.GetSetting().Enabled, WindowStart: startTs, WindowEnd: endTs,
		BucketSeconds: 3600, Series: []HourlyPoint{},
	}
	total := counters{}
	for ts := startTs; ts <= endTs; ts += 3600 {
		value := hourly[ts]
		total = sumCounters(total, value)
		result.Series = append(result.Series, hourlyPoint(ts, value))
	}
	result.Summary = summarize(total)
	return result, nil
}

func addHourly(hourly map[int64]counters, bucketTs int64, value counters) {
	hourTs := bucketTs - bucketTs%3600
	hourly[hourTs] = sumCounters(hourly[hourTs], value)
}

func sumCounters(a, b counters) counters {
	a.requestCount += b.requestCount
	a.successCount += b.successCount
	a.totalLatencyMs += b.totalLatencyMs
	a.ttftSumMs += b.ttftSumMs
	a.ttftCount += b.ttftCount
	a.outputTokens += b.outputTokens
	a.generationMs += b.generationMs
	return a
}

func hourlyPoint(ts int64, value counters) HourlyPoint {
	point := HourlyPoint{Ts: ts, RequestCount: value.requestCount, SuccessCount: value.successCount}
	if value.requestCount <= 0 {
		return point
	}
	rate := math.Round(successRate(value)*100) / 100
	latency := avg(value.totalLatencyMs, value.requestCount)
	point.SuccessRate, point.AvgLatencyMs = &rate, &latency
	if value.outputTokens > 0 && value.generationMs > 0 {
		tps := math.Round(avgTps(value)*100) / 100
		point.AvgTps = &tps
	}
	return point
}
