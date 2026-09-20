package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
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

func settleEpay(payment *epay.Payment, version *model.EpayMerchantVersion, callerIP string) (bool, error) {
	return model.RechargeEpay(model.EpayPayment{OrderNo: payment.OrderNo, MerchantID: payment.MerchantID, MerchantVersionID: version.ID, GatewayTradeNo: payment.TradeNo, AmountMinor: payment.AmountMinor, PaymentMethod: payment.Method}, callerIP)
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
	_, err = settleEpay(payment, merchant, callerIP)
	return err
}

type EpayPaymentService struct{ Client *epay.Client }

// Error categories are safe for operational logs and order query history.
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
	case errors.Is(err, context.DeadlineExceeded):
		return "QUERY_TIMEOUT"
	case errors.Is(err, epay.ErrInvalidResponse):
		return "INVALID_RESPONSE"
	case errors.Is(err, epay.ErrGatewayRejected):
		return "GATEWAY_REJECTED"
	case errors.Is(err, epay.ErrQueryFailed):
		return "QUERY_FAILED"
	case errors.Is(err, model.ErrEpaySnapshotMissing):
		return "SNAPSHOT_MISSING"
	case errors.Is(err, model.ErrTopUpNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "ORDER_NOT_FOUND"
	default:
		return "SETTLEMENT_FAILED"
	}
}

func (service EpayPaymentService) VerifyOrder(ctx context.Context, orderNo string, userID int, callerIP string) (*model.TopUp, error) {
	order, snapshot, merchant, err := model.GetEpayOrder(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if userID > 0 && order.UserId != userID {
		return nil, &EpayError{"NOT_FOUND", "订单不存在"}
	}
	if order.Status == common.TopUpStatusSuccess {
		return order, nil
	}
	if order.Status != common.TopUpStatusPending {
		return nil, model.ErrTopUpStatusInvalid
	}
	enabled, err := EpaySettlementEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, model.ErrEpaySettlementPaused
	}
	lease, err := model.ClaimEpayCheck(ctx, order.Id, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	client := service.Client
	if client == nil {
		client = epay.NewClient(nil)
	}
	payment, queryErr := client.QueryOrder(ctx, epay.Config{GatewayURL: merchant.GatewayURL, MerchantID: merchant.MerchantID, Key: merchant.Key}, order.TradeNo)
	if queryErr == nil {
		if payment.AmountMinor != snapshot.AmountMinor {
			queryErr = model.ErrEpayPaymentMismatch
		} else if payment.Status == epay.StatusTradeSuccess {
			_, queryErr = settleEpay(payment, merchant, callerIP)
		}
	}
	code := EpayFailureCode(queryErr)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finishErr := model.FinishEpayCheck(finishCtx, order.Id, lease, code, time.Now().Unix())
	if queryErr != nil {
		return nil, errors.Join(&EpayError{code, "支付结果暂时无法确认，请稍后重试"}, finishErr)
	}
	if finishErr != nil {
		return nil, finishErr
	}
	if err = model.DB.WithContext(ctx).First(order, order.Id).Error; err != nil {
		return nil, err
	}
	return order, nil
}

// The system task owns the cross-node batch lease; each order also has a lease
// shared with manual checks. Provider calls happen outside funds transactions.
func RecoverEpayOrders(ctx context.Context) (int, error) {
	return (EpayPaymentService{}).RecoverOrders(ctx)
}

func (service EpayPaymentService) RecoverOrders(ctx context.Context) (int, error) {
	enabled, err := EpaySettlementEnabled(ctx)
	if err != nil || !enabled {
		return 0, err
	}
	orders, err := model.PendingEpayChecks(ctx, time.Now().Unix(), 20)
	if err != nil {
		return 0, err
	}
	jobs := make(chan model.TopUp, len(orders))
	for _, order := range orders {
		jobs <- order
	}
	close(jobs)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failures []error
	completed := 0
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for order := range jobs {
				if ctx.Err() != nil {
					return
				}
				result, err := service.VerifyOrder(ctx, order.TradeNo, 0, "")
				mu.Lock()
				if err != nil && !errors.Is(err, model.ErrEpayCheckThrottled) {
					failures = append(failures, err)
				} else if result != nil && result.Status == common.TopUpStatusSuccess {
					completed++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return completed, errors.Join(append(failures, ctx.Err())...)
}
