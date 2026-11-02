package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
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

// DashboardLatencyRow 汇总所选范围消费日志的 use_time（秒）。
type DashboardLatencyRow struct {
	ModelName string
	Requests  int64
	UseTime   int64
}

// DashboardOutcome 按 user_id + request_id 去重：重试后成功的请求算一次成功。
type DashboardOutcome struct {
	Succeeded int64
	Failed    int64
}

// DashboardScope separates an explicitly authorized site query from a user query.
// Its zero value is invalid; an absent user ID must never expand the query.
type DashboardScope struct {
	userID int
	site   bool
}

func DashboardUserScope(userID int) DashboardScope { return DashboardScope{userID: userID} }
func DashboardSiteScope() DashboardScope           { return DashboardScope{site: true} }

func (scope DashboardScope) Validate() error {
	if !scope.site && scope.userID <= 0 {
		return errors.New("invalid dashboard user")
	}
	return nil
}

func (scope DashboardScope) validRange(start, end int64) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if start <= 0 || end <= start {
		return errors.New("invalid dashboard range")
	}
	return nil
}

func (scope DashboardScope) apply(tx *gorm.DB, column string) *gorm.DB {
	if scope.site {
		return tx
	}
	return tx.Where(column+" = ?", scope.userID)
}

// ReadDashboardQuota 读取起点落在 [start, end) 内的小时桶。
func ReadDashboardQuota(ctx context.Context, scope DashboardScope, start, end int64) ([]DashboardQuotaRow, error) {
	if err := scope.validRange(start, end); err != nil {
		return nil, err
	}
	rows := []DashboardQuotaRow{}
	err := scope.apply(DB.WithContext(ctx).Table("quota_data"), "user_id").
		Select("created_at, model_name, COALESCE(SUM(count),0) AS requests, COALESCE(SUM(quota),0) AS quota, COALESCE(SUM(token_used),0) AS tokens, COALESCE(SUM(cache_read_tokens),0) AS cache_read_tokens, COALESCE(SUM(cache_creation_tokens),0) AS cache_creation_tokens").
		Where("created_at >= ? AND created_at < ?", start, end).
		Group("created_at, model_name").
		Order("created_at, model_name").
		Scan(&rows).Error
	return rows, err
}

func ReadDashboardLatency(ctx context.Context, scope DashboardScope, start, end int64) ([]DashboardLatencyRow, error) {
	if err := scope.validRange(start, end); err != nil {
		return nil, err
	}
	if LOG_DB == nil {
		return nil, errors.New("log database is unavailable")
	}
	rows := []DashboardLatencyRow{}
	err := scope.apply(LOG_DB.WithContext(ctx).Table("logs"), "user_id").
		Select("model_name, COUNT(*) AS requests, COALESCE(SUM(use_time),0) AS use_time").
		Where("type = ? AND created_at >= ? AND created_at < ?", LogTypeConsume, start, end).
		Group("model_name").
		Scan(&rows).Error
	return rows, err
}

// ReadDashboardOutcome 按 user_id + request_id 去重。区间内的失败若在 end
// 之后结算，仍算成功；另一个用户的同名请求不能改变该请求的结果。
func ReadDashboardOutcome(ctx context.Context, scope DashboardScope, start, end int64) (DashboardOutcome, error) {
	var outcome DashboardOutcome
	if err := scope.validRange(start, end); err != nil {
		return outcome, err
	}
	if LOG_DB == nil {
		return outcome, errors.New("log database is unavailable")
	}
	query := func(alias string) *gorm.DB {
		return scope.apply(LOG_DB.WithContext(ctx).Table("logs AS "+alias), alias+".user_id")
	}
	inRangeErrors := query("attempt").Select("1").
		Where("attempt.user_id = logs.user_id AND attempt.request_id = logs.request_id").
		Where("attempt.type = ? AND attempt.created_at >= ? AND attempt.created_at < ?", LogTypeError, start, end)
	successes := query("logs").Select("logs.user_id, logs.request_id").
		Where("logs.type = ? AND logs.created_at >= ?", LogTypeConsume, start).
		Where("logs.request_id IS NOT NULL").
		Where("(logs.created_at < ? OR EXISTS (?))", end, inRangeErrors).
		Group("logs.user_id, logs.request_id")
	err := LOG_DB.WithContext(ctx).Table("(?) AS dashboard_requests", successes).Select("COUNT(*)").Scan(&outcome.Succeeded).Error
	if err != nil {
		return outcome, err
	}
	settled := query("settled").Select("1").
		Where("settled.user_id = logs.user_id AND settled.request_id = logs.request_id").
		Where("settled.type = ? AND settled.created_at >= ?", LogTypeConsume, start)
	failures := query("logs").Select("logs.user_id, logs.request_id").
		Where("logs.type = ? AND logs.created_at >= ? AND logs.created_at < ?", LogTypeError, start, end).
		Where("logs.request_id IS NOT NULL").
		Where("NOT EXISTS (?)", settled).
		Group("logs.user_id, logs.request_id")
	err = LOG_DB.WithContext(ctx).Table("(?) AS dashboard_requests", failures).Select("COUNT(*)").Scan(&outcome.Failed).Error
	return outcome, err
}
