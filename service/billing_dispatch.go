package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func prepareBillingDispatch(ctx *gin.Context, info *relaycommon.RelayInfo) error {
	if err := ensureBillingConsumptionRecovery(info); err != nil {
		return err
	}
	info.BillingAttemptID = common.NewRequestId()
	if err := model.PrepareBillingAttempt(billingRecoveryContext(ctx), info.RequestId, info.BillingAttemptID, info.ChannelId, info.PriceData.Quote); err != nil {
		return err
	}
	info.Billing.(*BillingSession).startRecoveryHeartbeat(billingRecoveryContext(ctx))
	return nil
}
