package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type HomeMetricBucket struct {
	Start    int64 `json:"start"`
	End      int64 `json:"end"`
	Requests int64 `json:"requests"`
}

type HomeRequestMetrics struct {
	Available      bool               `json:"available"`
	Requests24h    *int64             `json:"requests_24h"`
	HourlyRequests []int64            `json:"hourly_requests"`
	Buckets        []HomeMetricBucket `json:"buckets"`
	GeneratedAt    int64              `json:"generated_at"`
	Source         string             `json:"source"`
}

// The public page counts settled consumption records, never upstream attempts.
// Each of the 24 buckets has an explicit [start,end) boundary, without folding
// a partial twenty-fifth calendar hour into another hour.
func GetHomeRequestMetricsWithContext(ctx context.Context, now time.Time) (HomeRequestMetrics, error) {
	end := now.Unix() + 1
	start := end - 24*3600
	result := HomeRequestMetrics{Available: common.LogConsumeEnabled, HourlyRequests: make([]int64, 24), Buckets: make([]HomeMetricBucket, 24), GeneratedAt: now.Unix(), Source: "consume_logs"}
	for i := range result.Buckets {
		result.Buckets[i] = HomeMetricBucket{Start: start + int64(i)*3600, End: start + int64(i+1)*3600}
	}
	if !result.Available {
		return result, nil
	}
	if LOG_DB == nil {
		return result, errors.New("log database is unavailable")
	}
	var rows []struct {
		Bucket int64
		Count  int64
	}
	expression := "CAST((created_at - ?) / 3600 AS INTEGER)"
	switch common.LogDatabaseType() {
	case common.DatabaseTypePostgreSQL:
		expression = "(created_at - ?) / 3600"
	case common.DatabaseTypeMySQL:
		expression = "FLOOR((created_at - ?) / 3600)"
	case common.DatabaseTypeClickHouse:
		expression = "intDiv(created_at - ?, 3600)"
	}
	err := LOG_DB.WithContext(ctx).Table("logs").Select(expression+" AS bucket, COUNT(*) AS count", start).Where("type = ? AND created_at >= ? AND created_at < ?", LogTypeConsume, start, end).Group("bucket").Scan(&rows).Error
	if err != nil {
		return result, err
	}
	total := int64(0)
	for _, row := range rows {
		if row.Bucket >= 0 && row.Bucket < 24 {
			result.HourlyRequests[row.Bucket] = row.Count
			result.Buckets[row.Bucket].Requests = row.Count
			total += row.Count
		}
	}
	result.Requests24h = &total
	return result, nil
}
