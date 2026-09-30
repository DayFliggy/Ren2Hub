package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func prepareBillingDispatch(ctx *gin.Context, info *relaycommon.RelayInfo) error {
	if err := ensureBillingConsumptionRecovery(info); err != nil {
		return err
	}
	if info.BillingAttemptID == "" || info.PriceData.Usage == nil || info.PriceData.Charge == nil {
		return fmt.Errorf("billing estimate evidence is missing")
	}
	if err := model.PrepareBillingAttemptWithPlugin(billingRecoveryContext(ctx), info.RequestId, info.BillingAttemptID, info.ChannelId, info.PriceData.Quote, info.TaskPluginRef); err != nil {
		return err
	}
	if err := model.PersistBillingEstimate(info.BillingAttemptID, *info.PriceData.Usage, info.PriceData.Charge); err != nil {
		return err
	}
	info.Billing.(*BillingSession).startRecoveryHeartbeat(billingRecoveryContext(ctx))
	return nil
}
