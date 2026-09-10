package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/model"
)

type TaskSettlementRecoverySummary struct {
	Scanned   int `json:"scanned"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

func RecoverTaskSettlements(ctx context.Context, limit int) (TaskSettlementRecoverySummary, error) {
	var summary TaskSettlementRecoverySummary
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var receipts []model.TaskSettlement
	if err := model.DB.WithContext(ctx).Where("applied = ? OR cache_synced = ? OR log_delivered = ?", false, false, false).
		Order("task_id").Limit(limit).Find(&receipts).Error; err != nil {
		return summary, err
	}
	var failures []error
	for _, receipt := range receipts {
		if err := ctx.Err(); err != nil {
			return summary, errors.Join(append(failures, err)...)
		}
		summary.Scanned++
		_, err := model.ResumeTaskSettlement(ctx, receipt.TaskID)
		if err == nil {
			err = errors.Join(model.SyncTaskSettlementCache(ctx, receipt.TaskID), model.DeliverTaskSettlementLog(ctx, receipt.TaskID))
		}
		if err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("task settlement %d: %w", receipt.TaskID, err))
			continue
		}
		summary.Completed++
	}
	return summary, errors.Join(failures...)
}
