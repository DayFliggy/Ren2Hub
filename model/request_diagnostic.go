package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GatewayRequestDiagnostic is an expiring observation, never a billing or routing input.
type GatewayRequestDiagnostic struct {
	ID           int64  `json:"-" gorm:"primaryKey"`
	RequestID    string `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	UserID       int    `json:"-" gorm:"not null;index:idx_diagnostic_owner,priority:1;index:idx_diagnostic_trace,priority:1;index:idx_diagnostic_thread,priority:1"`
	TokenID      int    `json:"token_id" gorm:"not null;index:idx_diagnostic_trace,priority:2;index:idx_diagnostic_thread,priority:2"`
	TraceID      string `json:"trace_id,omitempty" gorm:"type:varchar(128);index:idx_diagnostic_trace,priority:3"`
	ThreadID     string `json:"thread_id,omitempty" gorm:"type:varchar(128);index:idx_diagnostic_thread,priority:3"`
	ModelName    string `json:"model_name" gorm:"type:varchar(200)"`
	CreatedAt    int64  `json:"created_at" gorm:"type:bigint;not null;index;index:idx_diagnostic_owner,priority:2"`
	WaitMS       int64  `json:"wait_ms" gorm:"type:bigint;not null"`
	UserQueueMS  int64  `json:"user_queue_ms" gorm:"type:bigint;not null"`
	FinalError   string `json:"final_error,omitempty" gorm:"type:varchar(64)"`
	AttemptsJSON string `json:"-" gorm:"type:text;not null"`
}

func SaveGatewayRequestDiagnostic(ctx context.Context, value *GatewayRequestDiagnostic) error {
	if value == nil || value.UserID <= 0 || len(value.RequestID) == 0 || len(value.RequestID) > 64 || len(value.TraceID) > 128 || len(value.ThreadID) > 128 || len(value.AttemptsJSON) > 64*1024 || value.CreatedAt <= 0 {
		return errors.New("invalid request diagnostic")
	}
	if DB == nil {
		return errors.New("diagnostic storage unavailable")
	}
	return DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(value).Error
}

type RequestDiagnosticFilter struct {
	UserID, TokenID              int
	RequestID, TraceID, ThreadID string
}

func QueryGatewayRequestDiagnostics(ctx context.Context, filter RequestDiagnosticFilter, offset, limit int, since int64) ([]GatewayRequestDiagnostic, int64, error) {
	items := []GatewayRequestDiagnostic{}
	var total int64
	if offset < 0 || limit < 1 || limit > 100 {
		return items, 0, errors.New("invalid diagnostic pagination")
	}
	query := DB.WithContext(ctx).Model(&GatewayRequestDiagnostic{}).Where("created_at >= ?", since)
	if filter.UserID > 0 {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.TokenID > 0 {
		query = query.Where("token_id = ?", filter.TokenID)
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if filter.TraceID != "" {
		query = query.Where("trace_id = ?", filter.TraceID)
	}
	if filter.ThreadID != "" {
		query = query.Where("thread_id = ?", filter.ThreadID)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

func GetGatewayRequestDiagnostic(ctx context.Context, requestID string, userID int, since int64) (*GatewayRequestDiagnostic, error) {
	items, _, err := QueryGatewayRequestDiagnostics(ctx, RequestDiagnosticFilter{UserID: userID, RequestID: requestID}, 0, 1, since)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &items[0], nil
}

func CleanupGatewayRequestDiagnostics(ctx context.Context, before time.Time) (int64, error) {
	var ids []int64
	if err := DB.WithContext(ctx).Model(&GatewayRequestDiagnostic{}).Where("created_at < ?", before.Unix()).Order("id").Limit(1000).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&GatewayRequestDiagnostic{})
	return result.RowsAffected, result.Error
}
