package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// dashboardBucketSeconds 与 quota_data 的写入粒度一致（LogQuotaData 只精确到小时）。
const dashboardBucketSeconds = 3600

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

// DashboardHour 是一个小时桶，Ts 为桶起点的 Unix 秒，序列按时间升序且不缺小时。
type DashboardHour struct {
	Ts       int64 `json:"ts"`
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
	Quota    int64 `json:"quota"`
}
type DashboardDay struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Consume  int64  `json:"consume"`
	Tokens   int64  `json:"tokens"`
}

// DashboardTokenDay 只含 quota_data 记录的事实：合计 token 和缓存读写 token。
// quota_data 没有输入/输出拆分，因此不提供这两项。
type DashboardTokenDay struct {
	Date        string `json:"date"`
	Total       int64  `json:"total"`
	CacheRead   int64  `json:"cache_read"`
	CacheCreate int64  `json:"cache_create"`
}

// DashboardCoverage 说明耗时和成功率能否从本人日志得出；不能时对应值为 null。
type DashboardCoverage struct {
	Latency     bool `json:"latency"`
	SuccessRate bool `json:"success_rate"`
}
type DashboardMeta struct {
	Start                int64             `json:"start"`
	End                  int64             `json:"end"`
	TZOffset             int               `json:"tz_offset"`
	GeneratedAt          int64             `json:"generated_at"`
	Available            bool              `json:"available"`
	Source               string            `json:"source"`
	BucketSeconds        int64             `json:"bucket_seconds"`
	FlushIntervalMinutes int               `json:"flush_interval_minutes"`
	Coverage             DashboardCoverage `json:"coverage"`
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

type dashboardWindow struct {
	userID     int
	start, end int64
}

// ParseDashboardRange returns day boundaries in loc, the viewer location from
// ParseViewerLocation.
func ParseDashboardRange(rangeName, startDate, endDate string, loc *time.Location, now time.Time) (time.Time, time.Time, error) {
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

// Dashboard 的请求数、token 和额度来自 quota_data（与原生 /api/data/self 同源）；
// 耗时和成功率只按本人日志计算，日志开关关闭时显式返回 null，不借用全站 perf_metrics。
func Dashboard(ctx context.Context, userID int, start, end time.Time, includeComparison bool) (DashboardSummary, error) {
	result := newDashboardSummary(start, end, includeComparison)
	window := dashboardWindow{userID: userID, start: start.Unix(), end: end.Unix()}
	if result.Meta.Available {
		rows, err := model.ReadDashboardQuota(ctx, userID, window.start, window.end)
		if err != nil {
			return result, err
		}
		if err := aggregateDashboard(&result, rows, start, end); err != nil {
			return result, err
		}
		if includeComparison {
			prior := dashboardWindow{userID: userID, start: start.Add(-end.Sub(start)).Unix(), end: window.start}
			if err := applyDashboardComparison(ctx, &result, prior); err != nil {
				return result, err
			}
		}
	}
	if err := applyDashboardLatency(ctx, &result, window); err != nil {
		return result, err
	}
	return result, applyDashboardOutcome(ctx, &result, window)
}

func newDashboardSummary(start, end time.Time, includeComparison bool) DashboardSummary {
	result := DashboardSummary{
		Models: []DashboardModel{}, Hourly: []DashboardHour{}, Flow: []DashboardDay{},
		TokenTrend: []DashboardTokenDay{}, Distribution: []DashboardDay{},
		Meta: DashboardMeta{
			Start: start.Unix(), End: end.Unix(), TZOffset: viewerOffset(start), GeneratedAt: time.Now().Unix(),
			Available: common.DataExportEnabled, Source: "quota_data",
			BucketSeconds: dashboardBucketSeconds, FlushIntervalMinutes: common.DataExportInterval,
			Coverage: DashboardCoverage{
				Latency:     common.LogConsumeEnabled,
				SuccessRate: common.LogConsumeEnabled && constant.ErrorLogEnabled,
			},
		},
	}
	if includeComparison {
		result.Comparison = &DashboardComparison{}
	}
	return result
}

func applyDashboardComparison(ctx context.Context, result *DashboardSummary, prior dashboardWindow) error {
	rows, err := model.ReadDashboardQuota(ctx, prior.userID, prior.start, prior.end)
	if err != nil {
		return err
	}
	var quota, requests int64
	for _, row := range rows {
		quota += row.Quota
		requests += row.Requests
	}
	result.Comparison = &DashboardComparison{
		QuotaDelta:    percentageChange(result.KPI.TotalQuota, quota),
		RequestsDelta: percentageChange(result.KPI.TotalRequests, requests),
	}
	return nil
}

// use_time 以秒记录，平均耗时换算为毫秒；没有样本时保持 null。
func applyDashboardLatency(ctx context.Context, result *DashboardSummary, window dashboardWindow) error {
	if !result.Meta.Coverage.Latency {
		return nil
	}
	rows, err := model.ReadDashboardLatency(ctx, window.userID, window.start, window.end)
	if err != nil {
		return err
	}
	byModel := make(map[string]*float64, len(rows))
	var requests, elapsed int64
	for _, row := range rows {
		requests += row.Requests
		elapsed += row.UseTime
		byModel[row.ModelName] = averageMilliseconds(row.UseTime, row.Requests)
	}
	result.KPI.AvgLatency = averageMilliseconds(elapsed, requests)
	for i := range result.Models {
		result.Models[i].AvgLatency = byModel[result.Models[i].Model]
	}
	return nil
}

func averageMilliseconds(seconds, count int64) *float64 {
	if count <= 0 {
		return nil
	}
	value := float64(seconds) * 1000 / float64(count)
	return &value
}

// 成功率按请求计算（0–100）：同一 request_id 失败后重试成功算一次成功。
// 只有消费日志和错误日志都开启时才有分母；否则或区间内没有请求时为 null。
func applyDashboardOutcome(ctx context.Context, result *DashboardSummary, window dashboardWindow) error {
	if !result.Meta.Coverage.SuccessRate {
		return nil
	}
	outcome, err := model.ReadDashboardOutcome(ctx, window.userID, window.start, window.end)
	if err != nil {
		return err
	}
	total := outcome.Succeeded + outcome.Failed
	if total == 0 {
		return nil
	}
	rate := float64(outcome.Succeeded) / float64(total) * 100
	result.KPI.SuccessRate = &rate
	return nil
}

func percentageChange(current, previous int64) *float64 {
	if previous == 0 {
		return nil
	}
	v := float64(current-previous) / float64(previous) * 100
	return &v
}

// aggregateDashboard 把 quota_data 的小时桶展开为连续小时序列，并按查看者时区归日。
// 小时桶整体计入其起点所在的日期（半小时时区下跨午夜的桶不拆分）。
func aggregateDashboard(result *DashboardSummary, rows []model.DashboardQuotaRow, start, end time.Time) error {
	result.Hourly = dashboardHours(start.Unix(), end.Unix())
	index := make(map[int64]int, len(result.Hourly))
	for i, hour := range result.Hourly {
		index[hour.Ts] = i
	}
	days := newDashboardDays(start, end)
	models := map[string]*DashboardModel{}
	for _, row := range rows {
		slot, ok := index[row.CreatedAt]
		if !ok {
			return fmt.Errorf("quota_data bucket %d is not an hour inside the dashboard range", row.CreatedAt)
		}
		hour := &result.Hourly[slot]
		hour.Requests += row.Requests
		hour.Tokens += row.Tokens
		hour.Quota += row.Quota
		result.KPI.TotalRequests += row.Requests
		result.KPI.TotalQuota += row.Quota
		result.KPI.TotalTokens += row.Tokens
		days.add(time.Unix(row.CreatedAt, 0).In(start.Location()).Format(time.DateOnly), row)
		addDashboardModel(models, row)
	}
	result.Models = sortedDashboardModels(models, result.KPI.TotalQuota)
	result.Flow, result.Distribution, result.TokenTrend = days.series()
	return nil
}

func dashboardHours(start, end int64) []DashboardHour {
	first := start - start%dashboardBucketSeconds
	if first < start {
		first += dashboardBucketSeconds
	}
	hours := []DashboardHour{}
	for ts := first; ts < end; ts += dashboardBucketSeconds {
		hours = append(hours, DashboardHour{Ts: ts})
	}
	return hours
}

type dashboardDays struct {
	order  []string
	usage  map[string]*DashboardDay
	tokens map[string]*DashboardTokenDay
}

func newDashboardDays(start, end time.Time) dashboardDays {
	days := dashboardDays{usage: map[string]*DashboardDay{}, tokens: map[string]*DashboardTokenDay{}}
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		days.order = append(days.order, key)
		days.usage[key] = &DashboardDay{Date: key}
		days.tokens[key] = &DashboardTokenDay{Date: key}
	}
	return days
}

func (days dashboardDays) add(key string, row model.DashboardQuotaRow) {
	usage, tokens := days.usage[key], days.tokens[key]
	usage.Requests += row.Requests
	usage.Consume += row.Quota
	usage.Tokens += row.Tokens
	tokens.Total += row.Tokens
	tokens.CacheRead += row.CacheReadTokens
	tokens.CacheCreate += row.CacheCreationTokens
}

func (days dashboardDays) series() ([]DashboardDay, []DashboardDay, []DashboardTokenDay) {
	flow := make([]DashboardDay, 0, len(days.order))
	tokens := make([]DashboardTokenDay, 0, len(days.order))
	for _, key := range days.order {
		flow = append(flow, *days.usage[key])
		tokens = append(tokens, *days.tokens[key])
	}
	return flow, append([]DashboardDay(nil), flow...), tokens
}

func addDashboardModel(models map[string]*DashboardModel, row model.DashboardQuotaRow) {
	item := models[row.ModelName]
	if item == nil {
		item = &DashboardModel{Model: row.ModelName}
		models[row.ModelName] = item
	}
	item.Requests += row.Requests
	item.Quota += row.Quota
	item.Tokens += row.Tokens
}

func sortedDashboardModels(models map[string]*DashboardModel, totalQuota int64) []DashboardModel {
	result := make([]DashboardModel, 0, len(models))
	for _, item := range models {
		if totalQuota != 0 {
			item.Share = float64(item.Quota) / float64(totalQuota) * 100
		}
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Quota != result[j].Quota {
			return result[i].Quota > result[j].Quota
		}
		return result[i].Model < result[j].Model
	})
	return result
}
