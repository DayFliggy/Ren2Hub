package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type DashboardKPI struct {
	TotalTokens   int64    `json:"totalTokens"`
	TotalQuota    int64    `json:"totalQuota"`
	TotalRequests int64    `json:"totalRequests"`
	AvgLatency    *float64 `json:"avgLatency"`
	SuccessRate   *float64 `json:"successRate"`
}
type DashboardComparison struct {
	QuotaDelta    *float64 `json:"quotaDelta"`
	RequestsDelta *float64 `json:"requestsDelta"`
}
type DashboardModel struct {
	Model      string   `json:"model"`
	Tokens     int64    `json:"tokens"`
	Quota      int64    `json:"quota"`
	Requests   int64    `json:"requests"`
	Share      float64  `json:"share"`
	AvgLatency *float64 `json:"avgLatency"`
}
type DashboardHour struct {
	Hour     string `json:"hour"`
	Requests int64  `json:"requests"`
}
type DashboardDay struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Consume  int64  `json:"consume"`
	Tokens   int64  `json:"tokens"`
}
type DashboardTokenDay struct {
	Date        string   `json:"date"`
	Total       int64    `json:"total"`
	Input       *int64   `json:"input"`
	Output      *int64   `json:"output"`
	CacheCreate *int64   `json:"cache_create"`
	CacheRead   *int64   `json:"cache_read"`
	HitRate     *float64 `json:"hit_rate"`
}
type DashboardMeta struct {
	Start         int64            `json:"start"`
	End           int64            `json:"end"`
	TZOffset      int              `json:"tz_offset"`
	GeneratedAt   int64            `json:"generated_at"`
	Available     bool             `json:"available"`
	Source        string           `json:"source"`
	Coverage      map[string]bool  `json:"coverage"`
	CacheCoverage map[string]int64 `json:"cache_coverage"`
}
type DashboardSummary struct {
	KPI          DashboardKPI         `json:"kpi"`
	Comparison   *DashboardComparison `json:"comparison"`
	Models       []DashboardModel     `json:"models"`
	Hourly       []DashboardHour      `json:"hourly"`
	Flow         []DashboardDay       `json:"flow"`
	TokenTrend   []DashboardTokenDay  `json:"token_trend"`
	Distribution []DashboardDay       `json:"distribution"`
	Meta         DashboardMeta        `json:"meta"`
}

// ParseDashboardRange uses the browser's Date.getTimezoneOffset convention.
func ParseDashboardRange(rangeName, startDate, endDate string, offset int, now time.Time) (time.Time, time.Time, error) {
	if offset < -14*60 || offset > 14*60 {
		return time.Time{}, time.Time{}, errors.New("invalid tz_offset")
	}
	loc := time.FixedZone("viewer", -offset*60)
	now = now.In(loc)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -1)
	switch rangeName {
	case "", "today":
	case "7d":
		start = end.AddDate(0, 0, -7)
	case "30d":
		start = end.AddDate(0, 0, -30)
	case "custom":
		var err error
		start, err = time.ParseInLocation("2006-01-02", startDate, loc)
		if err != nil {
			return start, end, errors.New("invalid start date")
		}
		last, err := time.ParseInLocation("2006-01-02", endDate, loc)
		if err != nil {
			return start, end, errors.New("invalid end date")
		}
		end = last.AddDate(0, 0, 1)
	default:
		return start, end, errors.New("invalid dashboard range")
	}
	if !end.After(start) || end.Sub(start) > 366*24*time.Hour || start.Unix() <= 0 {
		return start, end, errors.New("dashboard range must be 1 to 366 days")
	}
	return start, end, nil
}

func Dashboard(ctx context.Context, userID int, start, end time.Time, offset int, includeComparison bool) (DashboardSummary, error) {
	result := DashboardSummary{Models: []DashboardModel{}, Hourly: []DashboardHour{}, Flow: []DashboardDay{}, TokenTrend: []DashboardTokenDay{}, Distribution: []DashboardDay{}, Meta: DashboardMeta{Start: start.Unix(), End: end.Unix(), TZOffset: offset, GeneratedAt: time.Now().Unix(), Available: common.LogConsumeEnabled, Source: "consume_logs", Coverage: map[string]bool{"consume_logs": common.LogConsumeEnabled, "cache_usage": false, "latency": common.LogConsumeEnabled, "success_rate": false}}}
	if includeComparison {
		result.Comparison = &DashboardComparison{}
	}
	result.Meta.CacheCoverage = map[string]int64{"records": 0, "read_records": 0, "write_records": 0, "invalid_records": 0}
	if !result.Meta.Available {
		return result, nil
	}
	rows, err := model.ReadDashboardUsage(ctx, userID, start.Unix(), end.Unix())
	if err != nil {
		return result, err
	}
	result = aggregateDashboard(result, rows, start, end)
	cacheRows, err := model.ReadDashboardCacheUsage(ctx, userID, start.Unix(), end.Unix())
	if err != nil {
		return result, err
	}
	applyDashboardCache(&result, cacheRows, start)
	if !includeComparison {
		return result, nil
	}
	prior, err := model.ReadDashboardUsage(ctx, userID, start.Add(-end.Sub(start)).Unix(), start.Unix())
	if err != nil {
		return result, err
	}
	var previousQuota, previousRequests int64
	for _, row := range prior {
		previousQuota += row.Quota
		previousRequests += row.Requests
	}
	result.Comparison = &DashboardComparison{QuotaDelta: percentageChange(result.KPI.TotalQuota, previousQuota), RequestsDelta: percentageChange(result.KPI.TotalRequests, previousRequests)}
	return result, nil
}

func applyDashboardCache(result *DashboardSummary, rows []model.DashboardCacheRow, start time.Time) {
	result.Meta.CacheCoverage = map[string]int64{"records": result.KPI.TotalRequests, "read_records": 0, "write_records": 0, "invalid_records": 0}
	days := map[string]*DashboardTokenDay{}
	for i := range result.TokenTrend {
		days[result.TokenTrend[i].Date] = &result.TokenTrend[i]
	}
	for _, row := range rows {
		result.Meta.CacheCoverage["read_records"] += row.ReadRecords
		result.Meta.CacheCoverage["write_records"] += row.WriteRecords
		result.Meta.CacheCoverage["invalid_records"] += row.InvalidRecords
		day := days[start.Add(time.Duration(row.Bucket)*time.Hour).Format("2006-01-02")]
		if day == nil {
			continue
		}
		if row.ReadRecords > 0 {
			if day.CacheRead == nil {
				day.CacheRead = new(int64)
			}
			*day.CacheRead += row.Read
		}
		if row.WriteRecords > 0 {
			if day.CacheCreate == nil {
				day.CacheCreate = new(int64)
			}
			*day.CacheCreate += row.Write
		}
	}
	result.Meta.Coverage["cache_usage"] = result.Meta.CacheCoverage["read_records"] > 0 || result.Meta.CacheCoverage["write_records"] > 0
	// Native input semantics differ across protocols; no cross-protocol cache
	// hit denominator is inferred from prompt_tokens.
}
func percentageChange(current, previous int64) *float64 {
	if previous == 0 {
		return nil
	}
	v := float64(current-previous) / float64(previous) * 100
	return &v
}
func aggregateDashboard(result DashboardSummary, rows []model.DashboardUsageRow, start, end time.Time) DashboardSummary {
	models := map[string]*DashboardModel{}
	latencies := map[string]int64{}
	days := map[string]*DashboardDay{}
	tokens := map[string]*DashboardTokenDay{}
	hours := map[string]int64{}
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		days[key] = &DashboardDay{Date: key}
		in, out := int64(0), int64(0)
		tokens[key] = &DashboardTokenDay{Date: key, Input: &in, Output: &out}
	}
	var elapsed int64
	for _, row := range rows {
		result.KPI.TotalRequests += row.Requests
		result.KPI.TotalQuota += row.Quota
		result.KPI.TotalTokens += row.Input + row.Output
		elapsed += row.UseTime
		m := models[row.ModelName]
		if m == nil {
			m = &DashboardModel{Model: row.ModelName}
			models[row.ModelName] = m
		}
		m.Requests += row.Requests
		m.Quota += row.Quota
		m.Tokens += row.Input + row.Output
		latencies[row.ModelName] += row.UseTime
		at := start.Add(time.Duration(row.Bucket) * time.Hour)
		key := at.Format("2006-01-02")
		// Buckets start at viewer midnight, including half-hour timezone offsets.
		if day := days[key]; day != nil {
			day.Requests += row.Requests
			day.Consume += row.Quota
			day.Tokens += row.Input + row.Output
			t := tokens[key]
			*t.Input += row.Input
			*t.Output += row.Output
			t.Total += row.Input + row.Output
		}
		hours[at.Format("15:00")] += row.Requests
	}
	if result.KPI.TotalRequests > 0 {
		value := float64(elapsed) * 1000 / float64(result.KPI.TotalRequests)
		result.KPI.AvgLatency = &value
	}
	for _, m := range models {
		if result.KPI.TotalQuota != 0 {
			m.Share = float64(m.Quota) / float64(result.KPI.TotalQuota) * 100
		}
		if m.Requests > 0 {
			v := float64(latencies[m.Model]) * 1000 / float64(m.Requests)
			m.AvgLatency = &v
		}
		result.Models = append(result.Models, *m)
	}
	sort.Slice(result.Models, func(i, j int) bool {
		if result.Models[i].Quota != result.Models[j].Quota {
			return result.Models[i].Quota > result.Models[j].Quota
		}
		return result.Models[i].Model < result.Models[j].Model
	})
	for hour := 0; hour < 24; hour++ {
		key := time.Date(2000, 1, 1, hour, 0, 0, 0, time.UTC).Format("15:00")
		result.Hourly = append(result.Hourly, DashboardHour{Hour: key, Requests: hours[key]})
	}
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		result.Flow = append(result.Flow, *days[key])
		result.Distribution = append(result.Distribution, *days[key])
		result.TokenTrend = append(result.TokenTrend, *tokens[key])
	}
	return result
}
