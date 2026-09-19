package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func settleBillingConsumption(ctx *gin.Context, info *relaycommon.RelayInfo, params model.RecordConsumeLogParams, countUsage bool) {
	consumption, prepareErr := billingConsumptionSnapshot(ctx, info, params, countUsage)
	if prepareErr == nil {
		for attempt := 0; attempt < 3; attempt++ {
			prepareErr = model.PrepareBillingRecoveryConsumption(info.RequestId, consumption)
			if prepareErr == nil || errors.Is(prepareErr, model.ErrBillingRecoveryStateConflict) {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
			}
		}
	}
	// A failed log preparation must not refund already delivered usage. Persist
	// the monetary settlement even when the ancillary snapshot could not be saved.
	var settleErr error
	if info.Billing != nil {
		settleErr = SettleBilling(ctx, info, params.Quota)
	}
	if prepareErr != nil {
		prepareErr = errors.Join(prepareErr, model.RecordBillingConsumptionPreparationFailure(info.RequestId, prepareErr))
	}
	err := errors.Join(prepareErr, settleErr)
	if err == nil {
		err = model.ApplyBillingRecoveryConsumption(info.RequestId)
	}
	info.BillingSettlementError = err
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("billing consumption remains pending (request_id=%s, quota=%d): %v", info.RequestId, params.Quota, err))
	}
}

func billingConsumptionSnapshot(ctx *gin.Context, info *relaycommon.RelayInfo, params model.RecordConsumeLogParams, countUsage bool) (model.BillingConsumption, error) {
	if info.RequestId == "" {
		info.RequestId = common.NewRequestId()
	}
	ctx.Set(common.RequestIdKey, info.RequestId)
	if err := ensureBillingConsumptionRecovery(info); err != nil {
		return model.BillingConsumption{}, err
	}
	if info.ResponseUsage != nil && info.ResponseUsage.UsageSource != "" {
		if params.Other == nil {
			params.Other = make(map[string]interface{})
		}
		params.Other["usage_source"] = info.ResponseUsage.UsageSource
	}
	log, err := model.BuildConsumeLog(ctx, info.UserId, params)
	if err != nil {
		return model.BillingConsumption{}, err
	}
	return model.BillingConsumption{Log: *log, CountUsage: countUsage,
		LogEnabled: common.LogConsumeEnabled, ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled,
		NodeName: common.NodeName,
	}, nil
}

func ensureBillingConsumptionRecovery(info *relaycommon.RelayInfo) error {
	session, ok := info.Billing.(*BillingSession)
	if info.Billing != nil && !ok {
		return errors.New("billing session does not support durable consumption")
	}
	if session != nil && session.recoveryID != "" {
		if session.recoveryID != info.RequestId {
			return errors.New("billing consumption request identity conflict")
		}
		return nil
	}
	// Free and legacy callers may not have created a session before dispatch.
	// Attach their existing reservation to the same durable settlement path.
	reserved := info.FinalPreConsumedQuota
	if info.Billing != nil {
		reserved = info.Billing.GetPreConsumedQuota()
	}
	source := info.BillingSource
	if source == "" {
		source = BillingSourceWallet
	}
	tokenRequired := !info.IsPlayground && info.TokenId > 0
	tokenAmount := int64(0)
	if tokenRequired {
		tokenAmount = int64(reserved)
	}
	recovery, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: info.RequestId, UserID: info.UserId, TokenID: info.TokenId,
		Source: source, FundingAmount: int64(reserved), TokenAmount: tokenAmount, TokenRequired: tokenRequired,
	})
	if err != nil {
		return err
	}
	if recovery.PreConsumeState == model.BillingRecoveryPreConsumePrepared && recovery.Status == model.BillingRecoveryStatusActive {
		if err := model.UpdateBillingRecoveryAmounts(info.RequestId, int64(reserved), tokenAmount); err != nil {
			return err
		}
	}
	if info.Billing == nil {
		var funding FundingSource = &WalletFunding{userId: info.UserId, consumed: reserved}
		info.Billing = &BillingSession{relayInfo: info, funding: funding, recoveryID: info.RequestId, preConsumedQuota: reserved, tokenConsumed: int(tokenAmount)}
	} else {
		session.recoveryID = info.RequestId
	}
	return nil
}
