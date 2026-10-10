package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// Fallback reasons are fixed codes. Raw pricing errors can quote request
// values, so neither application logs nor consume logs copy their raw text.
const (
	billingFallbackUnpriced      = "usage_unpriced"
	billingFallbackPricingFailed = "pricing_failed"
	billingFallbackPrepareFailed = "settlement_prepare_failed"
	billingFallbackNoTerminal    = "request_ended_without_settlement"
	billingFallbackStale         = "recovery_stale_request"
)

// SettleBillingFallback charges a request whose final evidence cannot be
// priced. It uses the cumulative Realtime snapshot when one exists, otherwise
// the frozen pre-send estimate; a request without an estimate was never sent.
func SettleBillingFallback(ctx *gin.Context, info *relaycommon.RelayInfo, reason string, cause error) {
	if session, ok := info.Billing.(*BillingSession); ok {
		session.mu.Lock()
		session.settlementStarted = true
		session.stopRecoveryHeartbeat()
		session.mu.Unlock()
	}
	settleBillingFallback(ctx, info, reason, cause)
}

// settleBillingFallback requires the caller to have stopped the session from
// releasing or settling the same request concurrently.
func settleBillingFallback(ctx *gin.Context, info *relaycommon.RelayInfo, reason string, cause error) {
	if cause != nil {
		logger.LogError(ctx, fmt.Sprintf("billing settles from persisted evidence (request_id=%s, reason=%s)", info.RequestId, reason))
	}
	template := billingFallbackLog(reason, common.NewMessage("Settled at the pre-send estimate"))
	template.Group, template.ModelName, template.IsStream = info.UsingGroup, info.BillingModelName(), info.IsStream
	template.UseTime = int(common.GetTimestamp() - info.StartTime.Unix())
	if ctx != nil {
		template.TokenName, template.Username = ctx.GetString("token_name"), ctx.GetString("username")
	}
	_, err := finishActiveBilling(billingRecoveryContext(ctx), info.RequestId, info.BillingAttemptID, template)
	info.BillingSettlementError = err
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("billing fallback remains pending (request_id=%s): %v", info.RequestId, err))
	}
}

// billingFallbackLog is the log of a request settled at its frozen pre-send
// estimate: content shows how it was settled, admin_info records why.
func billingFallbackLog(reason string, content *common.Message) model.Log {
	other := map[string]any{"admin_info": map[string]any{"billing_fallback_reason": reason}}
	text := model.SetLogContent(other, []*common.Message{content})
	encoded, err := common.Marshal(other)
	if err != nil {
		return model.Log{Content: text}
	}
	return model.Log{Content: text, Other: string(encoded)}
}

// finishActiveBilling is the only bill-first decision for a request without a
// settled charge, in process and in recovery (attemptID is empty there).
// Dispatch records the attempt before provider I/O, so an empty consumption
// state proves that nothing was sent.
func finishActiveBilling(ctx context.Context, requestID, attemptID string, template model.Log) (string, error) {
	recovery, err := loadBillingRecovery(requestID)
	if err != nil {
		return "", err
	}
	switch {
	case recovery.Status != model.BillingRecoveryStatusActive:
		// A concurrent settlement or recovery already decided this request.
		return recoverOneBillingRecovery(ctx, requestID)
	case recovery.PreConsumeState == model.BillingRecoveryPreConsumePrepared:
		return model.BillingRecoveryStatusRefunded, model.AbortPreparedBillingRecovery(requestID, nil)
	case recovery.ConsumptionState == model.BillingRecoveryStateTaskPending:
		// The accepted task document owns its terminal accounting.
		return recovery.Status, nil
	case recovery.ConsumptionState == "":
		return releaseUnsentBilling(requestID)
	}
	err = model.FinalizeCollectedBillingConsumption(requestID)
	if err != nil {
		if !errors.Is(err, model.ErrNoCollectedBillingUsage) {
			logger.LogError(ctx, fmt.Sprintf("collected billing usage cannot settle (request_id=%s): %v", requestID, err))
		}
		err = model.PrepareFrozenEstimateConsumption(requestID, attemptID, template, true)
	}
	switch {
	case errors.Is(err, model.ErrBillingEstimateMissing):
		return releaseUnsentBilling(requestID)
	case errors.Is(err, model.ErrBillingTaskPending):
		// The accepted task document owns its terminal accounting.
		return model.BillingRecoveryStatusActive, nil
	case err != nil:
		return model.BillingRecoveryStatusActive, err
	}
	return recoverBillingSettlement(requestID)
}
