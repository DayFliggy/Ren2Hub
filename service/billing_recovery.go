package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	defaultBillingRecoveryStaleSeconds = 300
	defaultBillingRecoveryInterval     = 30 * time.Second
	defaultBillingRecoveryBatchLimit   = 100
)

func BillingRecoveryHeartbeatInterval() time.Duration {
	interval := BillingRecoveryStaleAfter() / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return interval
}

type BillingRecoverySummary struct {
	Scanned  int `json:"scanned"`
	Settled  int `json:"settled"`
	Refunded int `json:"refunded"`
	Pending  int `json:"pending"`
	Failed   int `json:"failed"`
}

func BillingRecoveryTaskEnabled() bool {
	return common.GetEnvOrDefaultBool("BILLING_RECOVERY_TASK_ENABLED", true)
}

func BillingRecoveryTaskInterval() time.Duration {
	seconds := common.GetEnvOrDefault("BILLING_RECOVERY_TASK_INTERVAL_SECONDS", int(defaultBillingRecoveryInterval/time.Second))
	if seconds < 5 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

func BillingRecoveryStaleAfter() time.Duration {
	seconds := common.GetEnvOrDefault("BILLING_RECOVERY_STALE_SECONDS", defaultBillingRecoveryStaleSeconds)
	if seconds < 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func BillingRecoveryBatchLimit() int {
	limit := common.GetEnvOrDefault("BILLING_RECOVERY_BATCH_LIMIT", defaultBillingRecoveryBatchLimit)
	if limit <= 0 || limit > 100 {
		return defaultBillingRecoveryBatchLimit
	}
	return limit
}

// RecoverStaleBillingRecoveries resumes durable accounting after a process
// restart. Each adjustment is fenced in model.ApplyBillingRecoveryAdjustment,
// so rerunning this pass cannot apply a balance change twice.
func RecoverStaleBillingRecoveries(ctx context.Context, cutoff int64, limit int) (BillingRecoverySummary, error) {
	var summary BillingRecoverySummary
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := model.ListStaleBillingRecoveries(cutoff, limit)
	if err != nil {
		return summary, err
	}
	for _, recovery := range rows {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.Scanned++
		status, err := recoverOneBillingRecovery(ctx, recovery.RequestID)
		if err != nil {
			summary.Failed++
			continue
		}
		switch status {
		case model.BillingRecoveryStatusSettled:
			summary.Settled++
		case model.BillingRecoveryStatusRefunded:
			summary.Refunded++
		default:
			summary.Pending++
		}
	}
	return summary, nil
}

func recoverOneBillingRecovery(ctx context.Context, requestID string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var recovery model.BillingRecovery
	if model.DB == nil {
		return "", errors.New("billing recovery database is unavailable")
	}
	if err := model.DB.WithContext(ctx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return "", err
	}
	switch recovery.Status {
	case model.BillingRecoveryStatusSettlementPending:
		return recoverBillingSettlement(recovery)
	case model.BillingRecoveryStatusActive, model.BillingRecoveryStatusRefundPending:
		return recoverBillingRefund(recovery)
	default:
		return recovery.Status, nil
	}
}

func recoverBillingSettlement(recovery model.BillingRecovery) (string, error) {
	current, err := loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return recovery.Status, err
	}
	recovery = current
	if recovery.FundingSettlement != model.BillingRecoveryStateCompleted {
		if err := model.ApplyBillingRecoveryAdjustment(recovery.RequestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationSettle); err != nil {
			return recovery.Status, err
		}
	}
	current, err = loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return recovery.Status, err
	}
	recovery = current
	if recovery.TokenSettlement != model.BillingRecoveryStateCompleted {
		if err := model.ApplyBillingRecoveryAdjustment(recovery.RequestID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle); err != nil {
			return recovery.Status, err
		}
	}
	current, err = loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return recovery.Status, err
	}
	if !model.BillingRecoverySettlementComplete(current) {
		return current.Status, errors.New("billing recovery settlement remains incomplete")
	}
	if err := model.MarkBillingRecoverySettled(current.RequestID); err != nil {
		return current.Status, err
	}
	return model.BillingRecoveryStatusSettled, nil
}

func recoverBillingRefund(recovery model.BillingRecovery) (string, error) {
	if err := model.MarkBillingRecoveryRefundPending(recovery.RequestID, nil); err != nil {
		return recovery.Status, err
	}
	current, err := loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return model.BillingRecoveryStatusRefundPending, err
	}
	recovery = current
	if recovery.FundingRefund != model.BillingRecoveryStateCompleted {
		if err := model.ApplyBillingRecoveryAdjustment(recovery.RequestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationRefund); err != nil {
			return model.BillingRecoveryStatusRefundPending, err
		}
	}
	current, err = loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return model.BillingRecoveryStatusRefundPending, err
	}
	recovery = current
	if recovery.ExtraAmount > 0 && recovery.ExtraRefund != model.BillingRecoveryStateCompleted {
		if err := model.ApplyBillingRecoveryAdjustment(recovery.RequestID, model.BillingRecoveryComponentExtra, model.BillingRecoveryOperationRefund); err != nil {
			return model.BillingRecoveryStatusRefundPending, err
		}
	}
	current, err = loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return model.BillingRecoveryStatusRefundPending, err
	}
	recovery = current
	if recovery.TokenAmount > 0 && recovery.TokenRefund != model.BillingRecoveryStateCompleted {
		if err := model.ApplyBillingRecoveryAdjustment(recovery.RequestID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationRefund); err != nil {
			return model.BillingRecoveryStatusRefundPending, err
		}
	}
	current, err = loadBillingRecovery(recovery.RequestID)
	if err != nil {
		return model.BillingRecoveryStatusRefundPending, err
	}
	if !model.BillingRecoveryRefundComplete(current) {
		return current.Status, errors.New("billing recovery refund remains incomplete")
	}
	if err := model.MarkBillingRecoveryRefunded(current.RequestID, nil); err != nil {
		return current.Status, err
	}
	return model.BillingRecoveryStatusRefunded, nil
}

func loadBillingRecovery(requestID string) (model.BillingRecovery, error) {
	var recovery model.BillingRecovery
	if model.DB == nil {
		return recovery, errors.New("billing recovery database is unavailable")
	}
	err := model.DB.Where("request_id = ?", requestID).First(&recovery).Error
	return recovery, err
}
