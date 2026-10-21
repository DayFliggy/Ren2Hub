package service

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"gorm.io/gorm"
)

// SettleGmpayNotification verifies a callback against the credentials the order
// was created with, then settles it. A callback carries no network and no
// currency. The snapshot records the requested asset, but cannot prove the
// final network selected in the hosted cashier. RechargeGmpay checks the token,
// fiat amount, gateway trade number and receipt under the order lock; this
// function checks the signature, order number and successful status.
func SettleGmpayNotification(ctx context.Context, params map[string]string, callerIP string) error {
	orderNo := params["order_id"]
	if orderNo == "" {
		return gmpay.ErrInvalidParameters
	}
	_, _, merchant, err := model.GetGmpayOrder(ctx, orderNo)
	if err != nil {
		return err
	}
	notification, err := gmpay.VerifyNotification(params, gmpay.Config{
		GatewayURL: merchant.GatewayURL, MerchantID: merchant.MerchantID, Key: merchant.Key,
	})
	if err != nil {
		return err
	}
	if notification.OrderNo != orderNo {
		return model.ErrGmpayPaymentMismatch
	}
	amountMinor, err := gmpay.ParseFiatAmount(notification.Amount)
	if err != nil {
		return model.ErrGmpayPaymentMismatch
	}
	if notification.Status != gmpay.StatusPaySuccess {
		// Only a completed payment earns the gateway's success acknowledgement.
		return gmpay.ErrInvalidParameters
	}
	_, err = model.RechargeGmpay(model.GmpayPayment{
		OrderNo: notification.OrderNo, MerchantID: notification.PID, MerchantVersionID: merchant.ID,
		GatewayTradeNo: notification.TradeNo, AmountMinor: amountMinor, Token: strings.ToLower(notification.Token),
		ActualAmount: notification.ActualAmount, ReceiveAddress: notification.ReceiveAddress,
		BlockTransactionID: notification.BlockTransactionID,
	}, callerIP)
	return err
}

// Callback rejection categories must not expose merchant credentials or raw errors.
func GmpayFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, model.ErrGmpayPaymentMismatch), errors.Is(err, model.ErrTopUpReceiptMismatch):
		return "PAYMENT_MISMATCH"
	case errors.Is(err, gmpay.ErrInvalidSignature):
		return "INVALID_SIGNATURE"
	case errors.Is(err, gmpay.ErrInvalidParameters):
		return "INVALID_PARAMETERS"
	case errors.Is(err, model.ErrGmpaySnapshotMissing):
		return "SNAPSHOT_MISSING"
	case errors.Is(err, model.ErrTopUpNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "ORDER_NOT_FOUND"
	default:
		return "SETTLEMENT_FAILED"
	}
}
