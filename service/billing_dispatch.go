package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func prepareBillingDispatch(ctx *gin.Context, info *relaycommon.RelayInfo) error {
	if info.RequestId == "" {
		info.RequestId = common.NewRequestId()
		ctx.Set(common.RequestIdKey, info.RequestId)
	}
	if err := ensureBillingConsumptionRecovery(info); err != nil {
		return err
	}
	var err error
	if info.TieredBillingSnapshot != nil && info.TieredBillingSnapshot.BillingMode == "tiered_expr" {
		err = model.PrepareBillingPricing(info.RequestId, model.BillingPricingFailure{
			ChannelID: info.ChannelId, Model: info.BillingModelName(), Snapshot: *info.TieredBillingSnapshot,
		})
	} else {
		err = model.PrepareBillingDispatch(info.RequestId, model.BillingDispatchSnapshot{
			ChannelID: info.ChannelId, Model: info.BillingModelName(), Price: info.PriceData,
			OtherRatios: info.PriceData.OtherRatios(), QuotaPerUnit: common.QuotaPerUnit,
		})
	}
	if err != nil {
		return err
	}
	info.Billing.(*BillingSession).startRecoveryHeartbeat(billingRecoveryContext(ctx))
	return nil
}
