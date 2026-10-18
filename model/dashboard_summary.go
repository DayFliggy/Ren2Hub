package model

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

// DashboardUsageRow is an aggregate, not a copy of individual request records.
// Prompt/completion are native persisted usage fields; optional cache facts are
// queried separately so missing or malformed metadata never means zero usage.
type DashboardUsageRow struct {
	Bucket    int64
	ModelName string
	Requests  int64
	Quota     int64
	Input     int64
	Output    int64
	UseTime   int64
}

type DashboardCacheRow struct {
	Bucket                                    int64
	Read, Write                               int64
	ReadRecords, WriteRecords, InvalidRecords int64
}

func ReadDashboardCacheUsage(ctx context.Context, userID int, start, end int64) ([]DashboardCacheRow, error) {
	if userID <= 0 || start <= 0 || end <= start || LOG_DB == nil {
		return nil, errors.New("invalid dashboard cache query")
	}
	rows, err := LOG_DB.WithContext(ctx).Table("logs").Select("created_at, other").Where("user_id = ? AND type = ? AND created_at >= ? AND created_at < ?", userID, LogTypeConsume, start, end).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := map[int64]*DashboardCacheRow{}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var at int64
		var other string
		if err := rows.Scan(&at, &other); err != nil {
			return nil, err
		}
		bucket := (at - start) / 3600
		item := buckets[bucket]
		if item == nil {
			item = &DashboardCacheRow{Bucket: bucket}
			buckets[bucket] = item
		}
		if other == "" {
			continue
		}
		var values map[string]common.RawMessage
		if err := common.UnmarshalJsonStr(other, &values); err != nil {
			item.InvalidRecords++
			continue
		}
		read, hasRead, badRead := cacheUsageNumber(values, "cache_tokens")
		write, hasWrite, badWrite := cacheUsageNumber(values, "cache_write_tokens")
		if !hasWrite && !badWrite {
			write, hasWrite, badWrite = cacheUsageNumber(values, "cache_creation_tokens")
		}
		if badRead || badWrite {
			item.InvalidRecords++
		}
		if hasRead {
			if read > math.MaxInt64-item.Read {
				return nil, errors.New("cache read aggregate overflow")
			}
			item.Read += read
			item.ReadRecords++
		}
		if hasWrite {
			if write > math.MaxInt64-item.Write {
				return nil, errors.New("cache write aggregate overflow")
			}
			item.Write += write
			item.WriteRecords++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]DashboardCacheRow, 0, len(buckets))
	for _, item := range buckets {
		result = append(result, *item)
	}
	return result, nil
}

func cacheUsageNumber(values map[string]common.RawMessage, key string) (int64, bool, bool) {
	raw, present := values[key]
	if !present {
		return 0, false, false
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, false, true
	}
	return value, true, false
}

func ReadDashboardUsage(ctx context.Context, userID int, start, end int64) ([]DashboardUsageRow, error) {
	if userID <= 0 || start <= 0 || end <= start {
		return nil, errors.New("invalid dashboard range or user")
	}
	if LOG_DB == nil {
		return nil, errors.New("log database is unavailable")
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
	rows := []DashboardUsageRow{}
	err := LOG_DB.WithContext(ctx).Table("logs").Select(expression+" AS bucket, model_name, COUNT(*) AS requests, COALESCE(SUM(quota),0) AS quota, COALESCE(SUM(prompt_tokens),0) AS input, COALESCE(SUM(completion_tokens),0) AS output, COALESCE(SUM(use_time),0) AS use_time", start).Where("user_id = ? AND type = ? AND created_at >= ? AND created_at < ?", userID, LogTypeConsume, start, end).Group("bucket, model_name").Order("bucket, model_name").Scan(&rows).Error
	return rows, err
}
