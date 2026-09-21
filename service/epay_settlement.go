package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/epay"
	"gorm.io/gorm"
)

func EpaySettlementEnabled(ctx context.Context) (bool, error) {
	var option model.Option
	err := model.DB.WithContext(ctx).Where(&model.Option{Key: "payment_setting.epay_settlement_enabled"}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(option.Value)
}

func SettleEpayNotification(ctx context.Context, params map[string]string, callerIP string) error {
	enabled, err := EpaySettlementEnabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return model.ErrEpaySettlementPaused
	}
	_, snapshot, merchant, err := model.GetEpayOrder(ctx, params["out_trade_no"])
	if err != nil {
		return err
	}
	payment, err := epay.VerifyNotification(params, epay.Config{GatewayURL: merchant.GatewayURL, MerchantID: merchant.MerchantID, Key: merchant.Key})
	if err != nil {
		return err
	}
	if payment.AmountMinor != snapshot.AmountMinor {
		return model.ErrEpayPaymentMismatch
	}
	if payment.Status != epay.StatusTradeSuccess {
		return nil
	}
	_, err = model.RechargeEpay(model.EpayPayment{
		OrderNo: payment.OrderNo, MerchantID: payment.MerchantID, MerchantVersionID: merchant.ID,
		GatewayTradeNo: payment.TradeNo, AmountMinor: payment.AmountMinor, PaymentMethod: payment.Method,
	}, callerIP)
	return err
}

// Callback rejection categories must not expose merchant credentials or raw errors.
func EpayFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, model.ErrEpaySettlementPaused):
		return "SETTLEMENT_PAUSED"
	case errors.Is(err, model.ErrEpayPaymentMismatch):
		return "PAYMENT_MISMATCH"
	case errors.Is(err, epay.ErrInvalidSignature):
		return "INVALID_SIGNATURE"
	case errors.Is(err, epay.ErrInvalidParameters):
		return "INVALID_PARAMETERS"
	case errors.Is(err, model.ErrEpaySnapshotMissing):
		return "SNAPSHOT_MISSING"
	case errors.Is(err, model.ErrTopUpNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "ORDER_NOT_FOUND"
	default:
		return "SETTLEMENT_FAILED"
	}
}
