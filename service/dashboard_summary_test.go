package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestDashboardHourlySeriesIsContinuousAndViewerDayAligned(t *testing.T) {
	now := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
	// UTC+5:30：查看者的午夜不在整点上。
	viewer, err := ParseViewerLocation("-330")
	require.NoError(t, err)
	start, end, err := ParseDashboardRange("7d", "", "", viewer, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 11, 18, 30, 0, 0, time.UTC).Unix(), start.Unix())
	firstHour := time.Date(2026, 10, 11, 19, 0, 0, 0, time.UTC).Unix()
	crossMidnight := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC).Unix() // 本地 23:30 开始
	rows := []model.DashboardQuotaRow{
		{CreatedAt: firstHour, ModelName: "b", Requests: 2, Quota: 5, Tokens: 14, CacheReadTokens: 3},
		{CreatedAt: crossMidnight, ModelName: "a", Requests: 1, Quota: 5, Tokens: 2, CacheCreationTokens: 7},
		{CreatedAt: crossMidnight, ModelName: "b", Requests: 1, Quota: 1, Tokens: 1},
	}
	summary := newDashboardSummary(start, end, false)
	require.NoError(t, aggregateDashboard(&summary, rows, start, end))
	require.Equal(t, -330, summary.Meta.TZOffset)

	require.Len(t, summary.Hourly, 7*24)
	for i, hour := range summary.Hourly {
		require.Equal(t, firstHour+int64(i)*3600, hour.Ts)
	}
	require.EqualValues(t, 2, summary.Hourly[0].Requests)
	require.EqualValues(t, 2, summary.Hourly[23].Requests)
	require.EqualValues(t, 0, summary.Hourly[1].Requests)

	require.EqualValues(t, 4, summary.KPI.TotalRequests)
	require.EqualValues(t, 17, summary.KPI.TotalTokens)
	require.EqualValues(t, 11, summary.KPI.TotalQuota)
	require.Len(t, summary.Flow, 7)
	require.Equal(t, "2026-10-12", summary.Flow[0].Date)
	require.EqualValues(t, 4, summary.Flow[0].Requests, "the 23:30 bucket counts toward the day it starts in")
	require.EqualValues(t, 3, summary.TokenTrend[0].CacheRead)
	require.EqualValues(t, 7, summary.TokenTrend[0].CacheCreate)
	require.Equal(t, summary.Flow, summary.Distribution)

	require.Equal(t, []string{"b", "a"}, []string{summary.Models[0].Model, summary.Models[1].Model})
	require.InDelta(t, 54.545, summary.Models[0].Share, 0.001)
	require.Nil(t, summary.KPI.SuccessRate)
	require.Nil(t, summary.KPI.AvgLatency)
}

func TestDashboardRejectsUnalignedQuotaBucket(t *testing.T) {
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	summary := newDashboardSummary(start, end, false)
	err := aggregateDashboard(&summary, []model.DashboardQuotaRow{{CreatedAt: start.Unix() + 60, Requests: 1}}, start, end)
	require.Error(t, err)
}

func TestDashboardRangeAndPercentageEdges(t *testing.T) {
	now := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
	_, _, err := ParseDashboardRange("custom", "2026-10-20", "2026-10-18", time.UTC, now)
	require.Error(t, err)
	require.Nil(t, percentageChange(1, 0))
	require.Equal(t, 100.0, *percentageChange(2, 1))
}
