package model

import (
	"context"
	"errors"
)

// DashboardQuotaRow 是 quota_data 按小时 × 模型的汇总。quota_data 只记录合计
// token，没有输入/输出拆分。
type DashboardQuotaRow struct {
	CreatedAt           int64
	ModelName           string
	Requests            int64
	Quota               int64
	Tokens              int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// DashboardLatencyRow 汇总本人消费日志的 use_time（秒）。
type DashboardLatencyRow struct {
	ModelName string
	Requests  int64
	UseTime   int64
}

// DashboardOutcome 按 request_id 去重计数：某渠道失败后重试成功的请求算一次成功。
type DashboardOutcome struct {
	Succeeded int64
	Failed    int64
}

func validDashboardRange(userID int, start, end int64) error {
	if userID <= 0 || start <= 0 || end <= start {
		return errors.New("invalid dashboard range or user")
	}
	return nil
}

// ReadDashboardQuota 读取起点落在 [start, end) 内的小时桶。
func ReadDashboardQuota(ctx context.Context, userID int, start, end int64) ([]DashboardQuotaRow, error) {
	if err := validDashboardRange(userID, start, end); err != nil {
		return nil, err
	}
	rows := []DashboardQuotaRow{}
	err := DB.WithContext(ctx).Table("quota_data").
		Select("created_at, model_name, COALESCE(SUM(count),0) AS requests, COALESCE(SUM(quota),0) AS quota, COALESCE(SUM(token_used),0) AS tokens, COALESCE(SUM(cache_read_tokens),0) AS cache_read_tokens, COALESCE(SUM(cache_creation_tokens),0) AS cache_creation_tokens").
		Where("user_id = ? AND created_at >= ? AND created_at < ?", userID, start, end).
		Group("created_at, model_name").
		Order("created_at, model_name").
		Scan(&rows).Error
	return rows, err
}

func ReadDashboardLatency(ctx context.Context, userID int, start, end int64) ([]DashboardLatencyRow, error) {
	if err := validDashboardRange(userID, start, end); err != nil {
		return nil, err
	}
	if LOG_DB == nil {
		return nil, errors.New("log database is unavailable")
	}
	rows := []DashboardLatencyRow{}
	err := LOG_DB.WithContext(ctx).Table("logs").
		Select("model_name, COUNT(*) AS requests, COALESCE(SUM(use_time),0) AS use_time").
		Where("user_id = ? AND type = ? AND created_at >= ? AND created_at < ?", userID, LogTypeConsume, start, end).
		Group("model_name").
		Scan(&rows).Error
	return rows, err
}

// ReadDashboardOutcome 按 request_id 给本人请求分类。区间内有错误、且
// start 之后任何时刻都没有消费日志的请求才算失败；同一请求在 end 之后
// 才结算的重试仍归为区间内请求的成功。
func ReadDashboardOutcome(ctx context.Context, userID int, start, end int64) (DashboardOutcome, error) {
	var outcome DashboardOutcome
	if err := validDashboardRange(userID, start, end); err != nil {
		return outcome, err
	}
	if LOG_DB == nil {
		return outcome, errors.New("log database is unavailable")
	}
	inRange := "user_id = ? AND type = ? AND created_at >= ? AND created_at < ?"
	inRangeErrors := LOG_DB.WithContext(ctx).Table("logs").Select("request_id").
		Where(inRange, userID, LogTypeError, start, end)
	err := LOG_DB.WithContext(ctx).Table("logs").Select("COUNT(DISTINCT request_id)").
		Where("user_id = ? AND type = ? AND created_at >= ?", userID, LogTypeConsume, start).
		Where("(created_at < ? OR request_id IN (?))", end, inRangeErrors).
		Scan(&outcome.Succeeded).Error
	if err != nil {
		return outcome, err
	}
	settled := LOG_DB.Table("logs").Select("request_id").
		Where("user_id = ? AND type = ? AND created_at >= ?", userID, LogTypeConsume, start)
	err = LOG_DB.WithContext(ctx).Table("logs").Select("COUNT(DISTINCT request_id)").
		Where(inRange, userID, LogTypeError, start, end).
		Where("request_id NOT IN (?)", settled).Scan(&outcome.Failed).Error
	return outcome, err
}
