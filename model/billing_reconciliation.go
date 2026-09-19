package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

var ErrBillingCommandInvalid = errors.New("invalid billing command")

type BillingResolution struct {
	OperationID     string       `json:"operation_id"`
	ExpectedVersion int64        `json:"expected_version"`
	Action          string       `json:"action"`
	Quota           common.Quota `json:"quota"`
	Reason          string       `json:"reason"`
}

type BillingRetryCommand struct {
	OperationID     string `json:"operation_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

func RecordBillingRetry(ctx context.Context, id int64, actor int, command BillingRetryCommand) (*BillingRecovery, error) {
	if actor <= 0 || strings.TrimSpace(command.OperationID) == "" || len(command.OperationID) > 150 {
		return nil, ErrBillingCommandInvalid
	}
	var result BillingRecovery
	err := BillingTransaction(ctx, func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&result, id).Error; err != nil {
			return err
		}
		fingerprint, err := billingEventFingerprint(struct {
			ID      int64
			Actor   int
			Command BillingRetryCommand
		}{id, actor, command})
		if err != nil {
			return err
		}
		eventID := "retry:" + command.OperationID
		if event, err := findBillingEvent(tx, eventID, fingerprint); err != nil || event != nil {
			return err
		}
		if result.Version != command.ExpectedVersion {
			return ErrBillingVersionConflict
		}
		if (result.Status == BillingRecoveryStatusRefunded || result.Status == BillingRecoveryStatusSettled) && result.ConsumptionState == BillingRecoveryStateCompleted {
			return ErrBillingRecoveryStateConflict
		}
		var user User
		if err := lockForUpdate(tx).First(&user, result.UserID).Error; err != nil {
			return err
		}
		event := BillingEvent{EventID: eventID, RequestID: result.RequestID, UserID: result.UserID, Operation: "retry", Fingerprint: fingerprint, ActorID: actor, Reason: "Retry saved accounting intent", CreatedAt: common.GetTimestamp(), AvailableAfter: user.Quota, ReservedAfter: user.ReservedQuota, UsedAfter: user.UsedQuota}
		if result.TokenRequired {
			var token Token
			if err := lockForUpdate(tx).First(&token, result.TokenID).Error; err != nil {
				return err
			}
			event.TokenID, event.TokenAvailableAfter, event.TokenReservedAfter, event.TokenUsedAfter = token.Id, token.RemainQuota, token.ReservedQuota, token.UsedQuota
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return tx.Model(&result).Updates(map[string]any{"version": result.Version + 1, "updated_at": common.GetTimestamp()}).Error
	})
	return &result, err
}

// ResolveBillingReconciliation is serialized with automatic recovery. Manual
// decisions leave the same immutable event as ordinary accounting operations.
func ResolveBillingReconciliation(ctx context.Context, id int64, actor int, command BillingResolution) (*BillingRecovery, error) {
	if actor <= 0 || strings.TrimSpace(command.OperationID) == "" || len(command.OperationID) > 150 || strings.TrimSpace(command.Reason) == "" || len(command.Reason) > 512 || (command.Action != "settle" && command.Action != "release") || command.Quota < 0 || command.Quota > common.MaxQuota || command.Action == "release" && command.Quota != 0 {
		return nil, ErrBillingCommandInvalid
	}
	var result BillingRecovery
	err := BillingTransaction(ctx, func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&result, id).Error; err != nil {
			return err
		}
		eventID := "manual:" + command.OperationID
		fingerprint, err := billingEventFingerprint(struct {
			RequestID, Operation string
			Quota                common.Quota
			Actor                int
			Reason               string
			ExpectedVersion      []int64
		}{result.RequestID, command.Action, command.Quota, actor, command.Reason, []int64{command.ExpectedVersion}})
		if err != nil {
			return err
		}
		if event, err := findBillingEvent(tx, eventID, fingerprint); err != nil || event != nil {
			return err
		}
		if result.Version != command.ExpectedVersion {
			return ErrBillingVersionConflict
		}
		if result.Status == BillingRecoveryStatusSettled || result.Status == BillingRecoveryStatusRefunded {
			return ErrBillingRecoveryStateConflict
		}
		// Keep the known attribution and task link when a Root supplies the
		// missing amount. Display delivery must use that same final amount.
		var consumption BillingConsumption
		if result.ConsumptionSnapshot != "" {
			if err := common.UnmarshalJsonStr(result.ConsumptionSnapshot, &consumption); err != nil {
				return err
			}
		}
		consumption.Log.RequestId, consumption.Log.UserId, consumption.Log.TokenId = result.RequestID, result.UserID, result.TokenID
		consumption.Log.Quota, consumption.Log.CreatedAt = command.Quota, common.GetTimestamp()
		consumption.Log.Content = "Manual reconciliation"
		consumption.Log.Type = LogTypeConsume
		consumption.CountUsage = command.Action == "settle"
		consumption.LogEnabled = common.LogConsumeEnabled
		consumption.ExportEnabled = common.DataExportEnabled
		if command.Action == "release" {
			consumption.Log.Type = LogTypeRefund
		}
		if consumption.Log.ChannelId == 0 {
			var attempt BillingAttempt
			err := tx.Where("request_id = ?", result.RequestID).Order("created_at desc, id desc").First(&attempt).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				consumption.Log.ChannelId = attempt.ChannelID
			}
		}
		if consumption.Log.ModelName == "" && result.QuoteSnapshot != "" {
			var quote billing.Quote
			if err := common.UnmarshalJsonStr(result.QuoteSnapshot, &quote); err != nil {
				return err
			}
			consumption.Log.ModelName = quote.PriceVersion.Model
		}
		if consumption.TaskAdjustment != nil {
			consumption.TaskAdjustment.FinalQuota = command.Quota
		}
		data, err := common.Marshal(consumption)
		if err != nil {
			return err
		}
		if err := tx.Model(&result).Updates(map[string]any{"consumption_snapshot": string(data), "consumption_state": BillingRecoveryStatePending, "consumption_statistics": BillingRecoveryStatePending, "consumption_log": BillingRecoveryStatePending, "consumption_error": ""}).Error; err != nil {
			return err
		}
		if err := finalizeBillingTx(tx, result.RequestID, command.Quota, command.Action == "release", eventID, actor, command.Reason, command.ExpectedVersion); err != nil {
			return err
		}
		return tx.First(&result, id).Error
	})
	return &result, err
}

type BillingMetrics struct {
	IdempotencyConflicts int64        `json:"idempotency_conflicts"`
	OldestWaitSeconds    int64        `json:"oldest_wait_seconds"`
	Pending              int64        `json:"pending"`
	Reconciliations      int64        `json:"reconciliations"`
	Reserved             common.Quota `json:"reserved_quota"`
	OldestCreatedAt      int64        `json:"oldest_created_at"`
	RecoveryFailures     int64        `json:"recovery_failures"`
	DebtEvents           int64        `json:"debt_events"`
}

func ReadBillingMetrics(ctx context.Context) (BillingMetrics, error) {
	var result BillingMetrics
	q := DB.WithContext(ctx)
	if err := q.Model(&BillingRecovery{}).Where("status IN ? OR (status IN ? AND consumption_state <> ?)", []string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending, BillingRecoveryStatusRefundPending, BillingRecoveryStatusReconciliation}, []string{BillingRecoveryStatusSettled, BillingRecoveryStatusRefunded}, BillingRecoveryStateCompleted).Select("COUNT(*) AS pending, COALESCE(MIN(created_at),0) AS oldest_created_at").Scan(&result).Error; err != nil {
		return result, err
	}
	if err := q.Model(&BillingRecovery{}).Where("status = ? OR consumption_state IN ?", BillingRecoveryStatusReconciliation, billingRecoveryUnconfirmedStates()).Count(&result.Reconciliations).Error; err != nil {
		return result, err
	}
	if err := q.Model(&User{}).Select("COALESCE(SUM(reserved_quota),0)").Scan(&result.Reserved).Error; err != nil {
		return result, err
	}
	if err := q.Model(&BillingRecovery{}).Where("attempts > 0 AND (status NOT IN ? OR consumption_state <> ?)", []string{BillingRecoveryStatusSettled, BillingRecoveryStatusRefunded}, BillingRecoveryStateCompleted).Count(&result.RecoveryFailures).Error; err != nil {
		return result, err
	}
	err := q.Model(&BillingEvent{}).Where("available_after < 0 AND used_delta > 0").Count(&result.DebtEvents).Error
	result.IdempotencyConflicts = billingIdempotencyConflicts.Load()
	if result.OldestCreatedAt > 0 {
		result.OldestWaitSeconds = max(0, common.GetTimestamp()-result.OldestCreatedAt)
	}
	return result, err
}
