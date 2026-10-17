package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/setting"
)

// GmpayOrderRequest contains native top-up calculation results, not a second
// pricing policy. The controller uses the same helpers as /api/user/amount.
type GmpayOrderRequest struct {
	Order         *model.TopUp
	ExpectedQuota int
	PayAmount     string
	ReturnURL     string
}

type GmpayCreatedOrder struct {
	Order      *model.TopUp
	Payment    *model.GmpayOrder
	PaymentURL string
}

func CreateGmpayTopUp(ctx context.Context, request GmpayOrderRequest, config setting.GmpayConfig) (*GmpayCreatedOrder, error) {
	if !config.Enabled || request.Order == nil {
		return nil, gmpay.ErrInvalidConfig
	}
	var asset *setting.GmpayAsset
	for i := range config.Assets {
		if config.Assets[i].Method() == request.Order.PaymentMethod {
			asset = &config.Assets[i]
			break
		}
	}
	if asset == nil {
		return nil, gmpay.ErrInvalidParameters
	}
	amountMinor, err := gmpay.ParseFiatAmount(request.PayAmount)
	if err != nil {
		return nil, err
	}
	order := request.Order
	order.TradeNo = "GP" + common.GetRandomString(30)
	order.PaymentProvider = model.PaymentProviderGmpay
	order.Status = common.TopUpStatusPending
	order.CreateTime = common.GetTimestamp()
	snapshot := &model.GmpayOrder{AmountMinor: amountMinor, Currency: "CNY", ExpectedQuota: request.ExpectedQuota, Token: asset.Token, Network: asset.Network}
	merchant := &model.GmpayMerchantVersion{GatewayURL: config.PayAddress, MerchantID: config.MerchantID, Key: config.SecretKey}
	outbound := gmpay.CreateOrder{OrderNo: order.TradeNo, Currency: "cny", Token: asset.Token, Network: asset.Network, Amount: request.PayAmount, NotifyURL: GetCallbackAddress() + "/api/user/gmpay/notify", ReturnURL: request.ReturnURL, Name: "Wallet top-up"}
	// Validate callbacks and precise gateway amount before making an order durable.
	if _, _, err := gmpay.BuildCreateOrder(config.Merchant(), outbound); err != nil {
		return nil, err
	}
	if err := model.CreateGmpayOrder(ctx, order, snapshot, merchant); err != nil {
		return nil, err
	}
	created, callErr := callGmpayCreateOrder(ctx, config.Merchant(), outbound)
	// Persist an unknown outcome even when the browser disconnected. No gateway
	// creation request is retried, because the remote operation is not idempotent.
	resultContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if callErr != nil {
		status := model.GmpayStatusUnknown
		if errors.Is(callErr, gmpay.ErrGatewayRejected) {
			status = model.GmpayStatusFailed
		}
		if markErr := model.MarkGmpayOrderResult(resultContext, order.Id, model.GmpayOrderResult{Status: status, Failure: gmpayFailureCode(callErr)}); markErr != nil {
			return nil, errors.Join(callErr, markErr)
		}
		return nil, callErr
	}
	if err := model.MarkGmpayOrderResult(resultContext, order.Id, model.GmpayOrderResult{Status: model.GmpayStatusReady, GatewayTradeNo: created.TradeNo, GatewayAmount: created.Amount, GatewayCryptoAmount: created.ActualAmount, ReceiveAddress: created.ReceiveAddress, ExpirationTime: created.ExpirationTime}); err != nil {
		return nil, err
	}
	return &GmpayCreatedOrder{Order: order, Payment: snapshot, PaymentURL: created.PaymentURL}, nil
}

// callGmpayCreateOrder performs the single outbound creation request. The
// gateway's create is not idempotent, so the caller must never retry it.
func callGmpayCreateOrder(ctx context.Context, config gmpay.Config, order gmpay.CreateOrder) (*gmpay.CreatedOrder, error) {
	endpoint, values, err := gmpay.BuildCreateOrder(config, order)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := gmpayHTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		return nil, gmpay.ErrInvalidParameters
	}
	// epusdt uses 400 for business rejection and 401 for signature rejection;
	// older deployments also use 200. Other HTTP statuses cannot establish the
	// outcome of creation, even when a proxy returns a known-looking error body.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusUnauthorized {
		return nil, gmpay.ErrInvalidParameters
	}
	created, err := gmpay.ParseCreatedOrder(body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, gmpay.ErrInvalidParameters
	}
	amount, amountErr := gmpay.ParseFiatAmount(created.Amount)
	expectedAmount, _ := gmpay.ParseFiatAmount(order.Amount)
	cryptoAmount, cryptoErr := gmpay.ParseTokenAmount(created.ActualAmount)
	if created.OrderNo != order.OrderNo || amountErr != nil || amount != expectedAmount ||
		!strings.EqualFold(created.Currency, order.Currency) || !strings.EqualFold(created.Token, order.Token) ||
		(created.Status != gmpay.StatusWaitPay && created.Status != gmpay.StatusPaySuccess) ||
		cryptoErr != nil || len(cryptoAmount.String()) > 64 ||
		strings.TrimSpace(created.ReceiveAddress) == "" || len(created.ReceiveAddress) > 256 || created.ExpirationTime <= 0 {
		return nil, gmpay.ErrInvalidParameters
	}
	// Persist the validated numeric value, so harmless padding cannot overflow
	// the receipt columns on databases that enforce their varchar limits.
	created.Amount = gmpay.FormatFiatAmount(amount)
	created.ActualAmount = cryptoAmount.String()
	return created, nil
}

// gmpayFailureCode reduces an outbound failure to a bounded operator-facing
// category. It never carries the merchant key or the raw upstream body.
func gmpayFailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, gmpay.ErrGatewayRejected):
		return "GATEWAY_REJECTED"
	case errors.Is(err, gmpay.ErrInvalidParameters):
		return "INVALID_RESPONSE"
	case errors.Is(err, gmpay.ErrInvalidConfig):
		return "INVALID_CONFIG"
	default:
		return "TRANSPORT"
	}
}
