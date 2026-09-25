package service

import (
	"errors"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// HoldUnconfirmedResponseUsage preserves the observed evidence and reservation;
// stream termination alone cannot turn estimated counters into actual charges.
func HoldUnconfirmedResponseUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	facts := NormalizedBillingUsage(usage, "openai")
	facts.Complete, facts.Estimated = false, true
	info.PriceData.Usage = &facts
	holdFailedBillingPrice(ctx, info, billingexpr.TokenParams{}, errors.New("final provider usage is unconfirmed"))
}

func holdFailedBillingPrice(ctx *gin.Context, info *relaycommon.RelayInfo, tokens billingexpr.TokenParams, cause error) {
	err := ensureBillingConsumptionRecovery(info)
	// Once delivered usage needs reconciliation, neither a deferred Refund nor
	// the recovery worker may convert its precharge into a guessed final amount.
	if session, ok := info.Billing.(*BillingSession); ok {
		session.mu.Lock()
		session.settlementStarted = true
		session.mu.Unlock()
		session.stopRecoveryHeartbeat()
	}
	if err == nil {
		err = model.RecordBillingCalculationFailure(info.RequestId, info.PriceData.Usage)
	}
	info.BillingSettlementError = errors.Join(cause, err)
	logger.LogError(ctx, "billing pricing requires reconciliation (request_id="+info.RequestId+"): "+"billing calculation failed")
}
