package model

import (
	"context"
	"time"
)

type RecentRouteModelUsage struct {
	ModelName    string `json:"model_name"`
	RequestCount int64  `json:"request_count"`
}

// ListRecentRelayModelUsage returns the low-cardinality model volume used by
// the administrator's Shadow exit diagnostics. It is intentionally separate
// from request-time routing and only reads aggregate log data.
func ListRecentRelayModelUsage(ctx context.Context, since time.Time) ([]RecentRouteModelUsage, error) {
	if LOG_DB == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var usage []RecentRouteModelUsage
	err := LOG_DB.WithContext(ctx).
		Model(&Log{}).
		Select("model_name, COUNT(*) AS request_count").
		Where("created_at >= ? AND type IN ? AND model_name <> ''", since.Unix(), []int{LogTypeConsume, LogTypeError}).
		Group("model_name").
		Order("request_count desc, model_name asc").
		Find(&usage).Error
	return usage, err
}
