package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestDashboardTimezoneAndUnknownCoverage(t *testing.T) {
	now := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
	start, end, err := ParseDashboardRange("today", "", "", -330, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 17, 18, 30, 0, 0, time.UTC).Unix(), start.Unix())
	require.Equal(t, 24*time.Hour, end.Sub(start))
	summary := aggregateDashboard(DashboardSummary{}, []model.DashboardUsageRow{{Bucket: 0, ModelName: "m", Requests: 2, Quota: 5, Input: 10, Output: 4, UseTime: 3}, {Bucket: 23, ModelName: "m", Requests: 1, Input: 2}}, start, end)
	require.EqualValues(t, 16, summary.KPI.TotalTokens)
	require.EqualValues(t, 3, summary.KPI.TotalRequests)
	require.Nil(t, summary.KPI.SuccessRate)
	require.Equal(t, 1000.0, *summary.KPI.AvgLatency)
	require.EqualValues(t, 12, *summary.TokenTrend[0].Input)
	require.Nil(t, summary.TokenTrend[0].CacheRead)
	require.Nil(t, percentageChange(1, 0))
	require.Equal(t, 100.0, *percentageChange(2, 1))
	_, _, err = ParseDashboardRange("custom", "2026-10-20", "2026-10-18", 0, now)
	require.Error(t, err)
}
