package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

// Bill-first fallbacks end a request whose final evidence cannot be priced.
// Dispatch persists the pre-send estimate before provider I/O, so a missing
// estimate proves that nothing was sent and the reservation may be released.
var ErrBillingEstimateMissing = errors.New("no frozen billing estimate was persisted")
var ErrNoCollectedBillingUsage = errors.New("no collected billing usage")
var ErrBillingTaskPending = errors.New("accepted task owns its terminal accounting")

// frozenBillingEstimateTx returns the attempt estimate bound to the frozen quote.
// An empty attemptID selects the latest attempt of that quote, as recovery does.
func frozenBillingEstimateTx(tx *gorm.DB, recovery BillingRecovery, attemptID string) (BillingAttempt, *billing.Usage, *billing.ChargeResult, error) {
	var quote billing.Quote
	if err := common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote); err != nil {
		return BillingAttempt{}, nil, nil, ErrBillingEstimateMissing
	}
	query := tx.Where("request_id = ? AND quote_hash = ? AND estimate_charge <> ''", recovery.RequestID, quote.Hash)
	if attemptID != "" {
		query = query.Where("id = ?", attemptID)
	}
	var attempt BillingAttempt
	err := query.Order("created_at desc").First(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BillingAttempt{}, nil, nil, ErrBillingEstimateMissing
	}
	if err != nil {
		return BillingAttempt{}, nil, nil, err
	}
	var usage billing.Usage
	var charge billing.ChargeResult
	if common.UnmarshalJsonStr(string(attempt.EstimateUsage), &usage) != nil || common.UnmarshalJsonStr(attempt.EstimateCharge, &charge) != nil {
		return BillingAttempt{}, nil, nil, billing.ErrEvaluationEvidence
	}
	return attempt, &usage, &charge, nil
}

// FrozenTaskBillingEstimate returns the submission estimate of an accepted
// task. A successful task whose completion cannot be priced settles at it.
func FrozenTaskBillingEstimate(requestID, attemptID string) (*billing.Usage, *billing.ChargeResult, error) {
	if DB == nil || requestID == "" || attemptID == "" {
		return nil, nil, ErrBillingEstimateMissing
	}
	var recovery BillingRecovery
	if err := DB.Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return nil, nil, err
	}
	_, usage, charge, err := frozenBillingEstimateTx(DB, recovery, attemptID)
	return usage, charge, err
}

// PrepareFrozenEstimateConsumption records a settlement intent for the
// attempt's own pre-send estimate. The template supplies display metadata;
// identity, quota and the charge evidence always come from the frozen attempt.
func PrepareFrozenEstimateConsumption(requestID, attemptID string, template Log, countUsage bool) error {
	if DB == nil || requestID == "" {
		return errors.New("invalid frozen billing estimate")
	}
	return BillingTransaction(context.Background(), func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status != BillingRecoveryStatusActive {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.ConsumptionState == BillingRecoveryStateTaskPending {
			return ErrBillingTaskPending
		}
		attempt, usage, charge, err := frozenBillingEstimateTx(tx, recovery, attemptID)
		if err != nil {
			return err
		}
		log, err := frozenEstimateLogTx(tx, recovery, attempt, charge, template)
		if err != nil {
			return err
		}
		consumption := BillingConsumption{AttemptID: attempt.ID, Usage: usage, Charge: charge, Log: log,
			CountUsage: countUsage, LogEnabled: common.LogConsumeEnabled,
			ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled, NodeName: common.NodeName}
		return prepareBillingRecoveryConsumption(tx, requestID, consumption, true)
	})
}

func frozenEstimateLogTx(tx *gorm.DB, recovery BillingRecovery, attempt BillingAttempt, charge *billing.ChargeResult, template Log) (Log, error) {
	log := template
	log.RequestId, log.UserId, log.TokenId = recovery.RequestID, recovery.UserID, recovery.TokenID
	log.ChannelId, log.Quota, log.Type = attempt.ChannelID, charge.FinalQuota, LogTypeConsume
	log.PromptTokens, log.CompletionTokens = 0, 0
	if log.CreatedAt == 0 {
		log.CreatedAt = common.GetTimestamp()
	}
	if log.Username == "" {
		var user User
		if err := tx.Select("username").First(&user, recovery.UserID).Error; err != nil {
			return Log{}, err
		}
		log.Username = user.Username
	}
	if log.TokenName == "" && recovery.TokenID > 0 {
		var token Token
		if err := tx.Select("name").First(&token, recovery.TokenID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return Log{}, err
		}
		log.TokenName = token.Name
	}
	if log.ModelName == "" {
		var quote billing.Quote
		if err := common.UnmarshalJsonStr(attempt.Quote, &quote); err != nil {
			return Log{}, err
		}
		log.ModelName = quote.PriceVersion.Model
	}
	other := map[string]any{}
	if log.Other != "" {
		if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
			return Log{}, fmt.Errorf("invalid billing log metadata: %w", err)
		}
	}
	// The displayed charge must be the frozen charge actually settled.
	other["charge_result"] = charge
	adminInfo, _ := other["admin_info"].(map[string]any)
	if adminInfo == nil {
		adminInfo = map[string]any{}
	}
	adminInfo["usage_billing_path"] = "frozen_estimate"
	other["admin_info"] = adminInfo
	encoded, err := common.Marshal(other)
	if err != nil {
		return Log{}, err
	}
	log.Other = string(encoded)
	return log, nil
}

// FinalizeCollectedBillingConsumption turns the latest cumulative Realtime
// snapshot into the final settlement intent when the session cannot price a
// newer observation.
func FinalizeCollectedBillingConsumption(requestID string) error {
	if DB == nil || requestID == "" {
		return errors.New("invalid collected billing usage")
	}
	return BillingTransaction(context.Background(), func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status != BillingRecoveryStatusActive || !BillingRecoveryUsageCollecting(recovery.ConsumptionState) || recovery.ConsumptionSnapshot == "" {
			return ErrNoCollectedBillingUsage
		}
		var consumption BillingConsumption
		var usage billing.Usage
		var charge billing.ChargeResult
		if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption); err != nil {
			return err
		}
		if err := common.UnmarshalJsonStr(string(recovery.UsageSnapshot), &usage); err != nil {
			return err
		}
		if err := common.UnmarshalJsonStr(recovery.ChargeSnapshot, &charge); err != nil {
			return err
		}
		consumption.Usage, consumption.Charge = &usage, &charge
		return prepareBillingRecoveryConsumption(tx, requestID, consumption, true)
	})
}
