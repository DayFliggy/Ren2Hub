package service

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func prepareBillingPriceGuard(ctx *gin.Context, info *relaycommon.RelayInfo) error {
	if info.TieredBillingSnapshot == nil || info.TieredBillingSnapshot.BillingMode != "tiered_expr" {
		return nil
	}
	if info.RequestId == "" {
		info.RequestId = common.NewRequestId()
		ctx.Set(common.RequestIdKey, info.RequestId)
	}
	if err := ensureBillingConsumptionRecovery(info); err != nil {
		return err
	}
	if err := model.PrepareBillingPricing(info.RequestId, model.BillingPricingFailure{
		ChannelID: info.ChannelId, Model: info.BillingModelName(), Snapshot: *info.TieredBillingSnapshot,
	}); err != nil {
		return err
	}
	info.Billing.(*BillingSession).startRecoveryHeartbeat(billingRecoveryContext(ctx))
	return nil
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
		failure := model.BillingPricingFailure{
			ChannelID: info.ChannelId, Model: info.BillingModelName(),
			Snapshot: *info.TieredBillingSnapshot, Tokens: tokens,
			Reason: "billing_expression_evaluation_failed",
		}
		for attempt := 0; attempt < 3; attempt++ {
			err = model.RecordBillingPricingFailure(info.RequestId, failure)
			if err == nil || errors.Is(err, model.ErrBillingRecoveryStateConflict) {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
			}
		}
	}
	info.BillingSettlementError = errors.Join(cause, err)
	logger.LogError(ctx, "billing pricing requires reconciliation (request_id="+info.RequestId+"): "+info.BillingSettlementError.Error())
}
