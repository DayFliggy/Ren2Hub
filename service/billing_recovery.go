package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
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
// restart or a request that ended without settlement. Each adjustment is
// fenced in the immutable BillingEvent, so rerunning cannot apply it twice.
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
			logger.LogError(ctx, fmt.Sprintf("billing recovery failed (request_id=%s): %v", recovery.RequestID, err))
			if deferErr := model.DeferBillingRecovery(recovery.RequestID); deferErr != nil {
				logger.LogError(ctx, fmt.Sprintf("billing recovery deferral failed (request_id=%s): %v", recovery.RequestID, deferErr))
			}
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
	recovery, err := loadBillingRecovery(requestID)
	if err != nil {
		return "", err
	}
	switch recovery.Status {
	case model.BillingRecoveryStatusSettlementPending:
		return recoverBillingSettlement(requestID)
	case model.BillingRecoveryStatusRefundPending:
		return recoverBillingRefund(requestID)
	case model.BillingRecoveryStatusSettled, model.BillingRecoveryStatusRefunded:
		return recovery.Status, model.ApplyBillingRecoveryConsumption(requestID)
	case model.BillingRecoveryStatusActive:
		return recoverActiveBilling(ctx, recovery)
	default:
		return recovery.Status, fmt.Errorf("unsupported billing recovery status %q", recovery.Status)
	}
}

// recoverActiveBilling finishes a stale request from durable evidence alone.
func recoverActiveBilling(ctx context.Context, recovery model.BillingRecovery) (string, error) {
	template := model.Log{Content: "恢复任务按发送前估算结算", Other: billingFallbackOther(billingFallbackStale)}
	return finishActiveBilling(ctx, recovery.RequestID, "", template)
}

func recoverBillingSettlement(requestID string) (string, error) {
	current, err := loadBillingRecovery(requestID)
	if err != nil {
		return "", err
	}
	actual, err := common.AddQuota(current.FundingAmount, current.SettlementDelta)
	if err != nil {
		return current.Status, err
	}
	if err := model.Settle(current.RequestID, actual); err != nil {
		return current.Status, err
	}
	return model.BillingRecoveryStatusSettled, model.ApplyBillingRecoveryConsumption(current.RequestID)
}

func recoverBillingRefund(requestID string) (string, error) {
	if err := model.MarkBillingRecoveryRefundPending(requestID, nil); err != nil {
		return model.BillingRecoveryStatusActive, err
	}
	return releaseBillingRecovery(requestID)
}

// releaseUnsentBilling releases a request on recovery's own evidence. The
// model rechecks under the row lock that no frozen estimate exists.
func releaseUnsentBilling(requestID string) (string, error) {
	if err := model.MarkUnsentBillingRecoveryRefundPending(requestID); err != nil {
		return model.BillingRecoveryStatusActive, err
	}
	return releaseBillingRecovery(requestID)
}

func releaseBillingRecovery(requestID string) (string, error) {
	if err := model.Release(requestID); err != nil {
		return model.BillingRecoveryStatusRefundPending, err
	}
	return model.BillingRecoveryStatusRefunded, model.ApplyBillingRecoveryConsumption(requestID)
}

func loadBillingRecovery(requestID string) (model.BillingRecovery, error) {
	var recovery model.BillingRecovery
	if model.DB == nil {
		return recovery, errors.New("billing recovery database is unavailable")
	}
	err := model.DB.Where("request_id = ?", requestID).First(&recovery).Error
	return recovery, err
}
