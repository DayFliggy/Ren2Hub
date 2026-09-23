package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// usdToQuotaUnit is the frozen quota-per-USD unit shared with the Epay rail.
var usdToQuotaUnit = decimal.NewFromInt(500000)

type GmpayQuoteRequest struct {
	Amount           int64  `json:"amount"`
	AmountUnit       string `json:"amount_unit"`
	PaymentMethod    string `json:"payment_method"`
	QuoteFingerprint string `json:"quote_fingerprint"`
}

// GmpayQuote is the frozen checkout offer. AmountMinor is the fiat amount in
// cents, which is the only monetary input the gateway signs and returns.
type GmpayQuote struct {
	Amount           int64        `json:"amount"`
	AmountUnit       string       `json:"amount_unit"`
	PaymentMethod    string       `json:"payment_method"`
	Token            string       `json:"token"`
	Network          string       `json:"network"`
	AmountMinor      int64        `json:"amount_minor,string"`
	PayAmount        string       `json:"pay_amount"`
	Currency         string       `json:"currency"`
	ExpectedQuota    common.Quota `json:"expected_quota"`
	MinTopUp         int64        `json:"min_topup"`
	MinimumQuantity  int64        `json:"minimum_quantity,string"`
	MinimumQuota     common.Quota `json:"minimum_quota"`
	QuoteFingerprint string       `json:"quote_fingerprint"`
	UnitPrice        string       `json:"-"`
	GroupRatio       string       `json:"-"`
	Discount         string       `json:"-"`
}

// BuildGmpayQuote prices one asset checkout and records the requested network.
// The gateway callback does not prove which network ultimately received payment.
func BuildGmpayQuote(config *GmpayConfig, userID int, group string, request GmpayQuoteRequest) (*GmpayQuote, error) {
	if !config.Enabled() {
		return nil, &GmpayError{"PAYMENT_UNAVAILABLE", "当前管理员未配置支付信息"}
	}
	asset, ok := config.FindAsset(request.PaymentMethod)
	if !ok {
		return nil, &GmpayError{"PAYMENT_UNAVAILABLE", "支付方式不存在"}
	}
	minimum, err := operation_setting.GmpayMinimumUSD(config.MinTopUp, asset)
	if err != nil {
		return nil, err
	}
	unit := request.AmountUnit
	if unit == "" {
		unit = "USD"
		if config.DisplayType == operation_setting.QuotaDisplayTypeTokens {
			unit = "TOKENS"
		}
	}
	if request.Amount <= 0 || (unit != "USD" && unit != "TOKENS") {
		return nil, &GmpayError{"VALIDATION_ERROR", "充值数量或单位无效"}
	}
	quota := decimal.NewFromInt(request.Amount)
	if unit == "USD" {
		quota = quota.Mul(usdToQuotaUnit)
	}
	expected, err := common.WalletQuotaFromDecimal(quota)
	if err != nil {
		return nil, &GmpayError{"VALIDATION_ERROR", "充值额度超出系统可表示范围"}
	}
	if expected < common.Quota(minimum*500000) {
		return nil, &GmpayError{"VALIDATION_ERROR", fmt.Sprintf("充值额度不能小于 %d USD", minimum)}
	}
	ratio, ok := config.Ratios[group]
	if !ok {
		ratio = decimal.NewFromInt(1)
	}
	// Presets are USD amounts regardless of the request's display unit.
	discount := decimal.NewFromInt(1)
	if expected%500000 == 0 {
		if configured, exists := config.Discounts[int64(expected)/500000]; exists {
			discount = configured
		}
	}
	if !ratio.IsPositive() || !discount.IsPositive() {
		return nil, errors.New("invalid gmpay pricing multiplier")
	}
	// The gateway is quoted in USD cents: the crypto quantity it derives is
	// evidence only and must never feed back into the credited quota.
	minor := quota.Mul(decimal.RequireFromString("0.000002")).Mul(config.Price).Mul(ratio).Mul(discount).Round(2).Mul(decimal.NewFromInt(100))
	if !minor.IsPositive() || minor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, &GmpayError{"VALIDATION_ERROR", "支付金额超出系统可表示范围"}
	}
	if minor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return nil, &GmpayError{"VALIDATION_ERROR", "支付金额必须大于 0.01 USD"}
	}
	quote := &GmpayQuote{
		// The quote echoes the bare asset the client submitted, matching the Epay
		// rail; only the persisted order carries the prefixed method id.
		Amount: request.Amount, AmountUnit: unit, PaymentMethod: asset.Token + "." + asset.Network,
		Token: asset.Token, Network: asset.Network, AmountMinor: minor.IntPart(), Currency: "USD",
		ExpectedQuota: expected, MinTopUp: minimum, UnitPrice: config.Price.String(),
		GroupRatio: ratio.String(), Discount: discount.String(),
	}
	quote.MinimumQuantity, quote.MinimumQuota = minimum, common.Quota(minimum*500000)
	if unit == "TOKENS" {
		quote.MinimumQuantity = int64(quote.MinimumQuota)
	}
	quote.PayAmount = gmpay.FormatFiatAmount(quote.AmountMinor)
	// A comparison token, not payment authority: every submission is recalculated.
	encoded, err := common.Marshal([]any{userID, group, quote, quote.UnitPrice, quote.GroupRatio, quote.Discount, config.Merchant.GatewayURL, config.Merchant.MerchantID, config.NotifyURL, config.ReturnURL})
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(config.Merchant.Key))
	_, _ = mac.Write(encoded)
	quote.QuoteFingerprint = hex.EncodeToString(mac.Sum(nil))
	return quote, nil
}

func QuoteGmpay(ctx context.Context, userID int, request GmpayQuoteRequest) (*GmpayQuote, *GmpayConfig, error) {
	config, err := LoadGmpayConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !config.Enabled() {
		return nil, nil, &GmpayError{"PAYMENT_UNAVAILABLE", "当前管理员未配置支付信息"}
	}
	var user model.User
	if err = model.DB.WithContext(ctx).Select("id", "group").First(&user, userID).Error; err != nil {
		return nil, nil, err
	}
	quote, err := BuildGmpayQuote(config, userID, user.Group, request)
	if err != nil {
		return nil, nil, err
	}
	if err = model.ValidateTopUpQuotaCapacity(userID, quote.ExpectedQuota); err != nil {
		if errors.Is(err, model.ErrTopUpQuotaLimitExceeded) {
			return nil, nil, &GmpayError{"VALIDATION_ERROR", "top-up quota limit exceeded"}
		}
		return nil, nil, err
	}
	return quote, config, nil
}

// GmpayCreatedOrder carries the checkout redirect plus the local order evidence.
type GmpayCreatedOrder struct {
	Order      *model.TopUp
	Quote      *GmpayQuote
	PaymentURL string
}

// gmpayTradeNo is exactly 32 characters, the documented gateway maximum, and
// deliberately independent of the user id: a longer user id must not overflow
// the gateway's order id bound.
func gmpayTradeNo() string {
	return "GP" + common.GetRandomString(30)
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

// CreateGmpayTopUp freezes the quote and its asset, then calls the gateway once.
// A creation outcome that cannot be determined keeps the same local order in the
// unknown state for operator review instead of silently creating a second one.
func CreateGmpayTopUp(ctx context.Context, userID int, request GmpayQuoteRequest) (*GmpayCreatedOrder, error) {
	quote, config, err := QuoteGmpay(ctx, userID, request)
	if err != nil {
		var business *GmpayError
		if request.QuoteFingerprint != "" && errors.As(err, &business) && (business.Code == "PAYMENT_UNAVAILABLE" || business.Code == "VALIDATION_ERROR") {
			return nil, &GmpayError{"QUOTE_CHANGED", "报价已变化，请重新确认"}
		}
		return nil, err
	}
	if request.QuoteFingerprint != "" && !hmac.Equal([]byte(request.QuoteFingerprint), []byte(quote.QuoteFingerprint)) {
		return nil, &GmpayError{"QUOTE_CHANGED", "报价已变化，请重新确认"}
	}
	if quote.AmountMinor <= 0 {
		return nil, errors.New("invalid gmpay payable amount")
	}
	base, err := gmpay.NormalizeGatewayURL(config.Merchant.GatewayURL)
	if err != nil {
		return nil, err
	}
	config.Merchant.GatewayURL = base

	order := &model.TopUp{
		UserId: userID, Amount: quote.Amount, Money: float64(quote.AmountMinor) / 100,
		TradeNo: gmpayTradeNo(), PaymentMethod: "gmpay:" + quote.Token + "." + quote.Network,
		PaymentProvider: model.PaymentProviderGmpay, CreateTime: time.Now().Unix(),
		Status: common.TopUpStatusPending,
	}
	if common.AffiliateEnabled && common.AffiliateActivatedAt > 0 {
		// The affiliate base is denominated in USD cents, matching the fiat
		// amount the gateway actually collects.
		affiliateBase, _ := decimal.NewFromInt(quote.AmountMinor).Mul(decimal.NewFromInt(5000)).QuoRem(config.Price, 0)
		order.AffiliateBaseQuota, err = common.WalletQuotaFromDecimal(affiliateBase)
		if err != nil || order.AffiliateBaseQuota <= 0 {
			return nil, errors.New("invalid affiliate payment base")
		}
	}
	snapshot := &model.GmpayOrder{
		InputAmount: quote.Amount, AmountUnit: quote.AmountUnit, AmountMinor: quote.AmountMinor,
		Currency: quote.Currency, ExpectedQuota: quote.ExpectedQuota, UnitPrice: quote.UnitPrice,
		GroupRatio: quote.GroupRatio, Discount: quote.Discount, Token: quote.Token,
		Network: quote.Network, QuoteFingerprint: quote.QuoteFingerprint,
	}
	merchant := &model.GmpayMerchantVersion{GatewayURL: base, MerchantID: config.Merchant.MerchantID, Key: config.Merchant.Key}
	gatewayOrder := gmpay.CreateOrder{
		OrderNo: order.TradeNo, Currency: "usd", Token: quote.Token, Network: quote.Network,
		Amount: quote.PayAmount, NotifyURL: config.NotifyURL, ReturnURL: config.ReturnURL,
		Name: fmt.Sprintf("TUC%d", quote.Amount),
	}
	if _, _, err = gmpay.BuildCreateOrder(config.Merchant, gatewayOrder); err != nil {
		return nil, &GmpayError{"VALIDATION_ERROR", "支付配置或金额不符合网关要求"}
	}
	if err = model.CreateGmpayOrder(ctx, order, snapshot, merchant); err != nil {
		return nil, err
	}

	created, callErr := callGmpayCreateOrder(ctx, config.Merchant, gatewayOrder)
	// The browser may disconnect after the gateway accepted the order. Persist
	// its outcome with a separate bounded context so the snapshot stays useful.
	resultContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if callErr != nil {
		failure := gmpayFailureCode(callErr)
		status := model.GmpayStatusUnknown
		if errors.Is(callErr, gmpay.ErrGatewayRejected) {
			status = model.GmpayStatusFailed
		}
		if markErr := model.MarkGmpayOrderResult(resultContext, order.Id, model.GmpayOrderResult{Status: status, Failure: failure}); markErr != nil {
			return nil, errors.Join(callErr, markErr)
		}
		return nil, callErr
	}
	if err = model.MarkGmpayOrderResult(resultContext, order.Id, model.GmpayOrderResult{
		Status: model.GmpayStatusReady, GatewayTradeNo: created.TradeNo,
		GatewayAmount: created.Amount, GatewayCryptoAmount: created.ActualAmount,
		ReceiveAddress: created.ReceiveAddress, ExpirationTime: created.ExpirationTime,
	}); err != nil {
		return nil, err
	}
	return &GmpayCreatedOrder{Order: order, Quote: quote, PaymentURL: created.PaymentURL}, nil
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
