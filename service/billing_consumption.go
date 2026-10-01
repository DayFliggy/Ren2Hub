package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// CompleteBillingConsumption is also used by administrator channel probes,
// which follow the same reservation and accounting rules as relayed requests.
func CompleteBillingConsumption(ctx *gin.Context, info *relaycommon.RelayInfo, params model.RecordConsumeLogParams, countUsage bool) error {
	settleBillingConsumption(ctx, info, params, countUsage)
	return info.BillingSettlementError
}

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
	// Wallet settlement never races ahead of its consumption evidence. Evidence
	// that cannot be persisted falls back to the frozen pre-send estimate.
	if prepareErr != nil {
		SettleBillingFallback(ctx, info, billingFallbackPrepareFailed, prepareErr)
		if info.BillingSettlementError != nil {
			info.BillingSettlementError = errors.Join(prepareErr, info.BillingSettlementError,
				model.RecordBillingConsumptionPreparationFailure(info.RequestId, prepareErr))
		}
		return
	}
	var err error
	if info.Billing != nil {
		err = SettleBilling(ctx, info, int(params.Quota))
	}
	if err == nil {
		billing.RecordSettlement(billing.SettlementBasis(consumption.Usage))
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
	return model.BillingConsumption{AttemptID: info.BillingAttemptID, Log: *log, CountUsage: countUsage, Usage: info.PriceData.Usage, Charge: info.PriceData.Charge,
		LogEnabled: common.LogConsumeEnabled, ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled,
		NodeName: common.NodeName,
	}, nil
}

func ensureBillingConsumptionRecovery(info *relaycommon.RelayInfo) error {
	session, ok := info.Billing.(*BillingSession)
	if !ok || session.recoveryID == "" {
		return errors.New("durable reservation must exist before dispatch")
	}
	if session.recoveryID != info.RequestId {
		return errors.New("billing request identity conflict")
	}
	return nil
}
