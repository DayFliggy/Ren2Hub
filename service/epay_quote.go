package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm/clause"
)

type EpayError struct {
	Code    string
	Message string
}

func (err *EpayError) Error() string { return err.Message }

type EpayConfig struct {
	Merchant      epay.Config `json:"-"`
	Methods       []map[string]string
	MinTopUp      int64
	Price         decimal.Decimal
	Ratios        map[string]decimal.Decimal
	Discounts     map[int64]decimal.Decimal
	AmountOptions []int
	DisplayType   string
	NotifyURL     string
	ReturnURL     string
	Compliance    bool
}

// A single SELECT sees a committed configuration, including bulk merchant
// rotation. Reading mutable process globals could mix old and new credentials.
func LoadEpayConfig(ctx context.Context) (*EpayConfig, error) {
	values := map[string]string{
		"PayAddress": "", "EpayId": "", "EpayKey": "", "CustomCallbackAddress": "", "ServerAddress": "http://localhost:3000",
		"PayMethods": `[{"name":"支付宝","type":"alipay"},{"name":"微信","type":"wxpay"},{"name":"自定义1","type":"custom1","min_topup":"50"}]`,
		"Price":      "7.3", "MinTopUp": "1", "TopupGroupRatio": `{"default":1,"vip":1,"svip":1}`,
		"payment_setting.amount_options": "[10,20,50,100,200,500]", "payment_setting.amount_discount": "{}",
		"payment_setting.compliance_confirmed": "false", "payment_setting.compliance_terms_version": "",
		"general_setting.quota_display_type": "USD",
	}
	keys := make([]any, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	var options []model.Option
	if err := model.DB.WithContext(ctx).Where(clause.IN{Column: clause.Column{Name: "key"}, Values: keys}).Find(&options).Error; err != nil {
		return nil, err
	}
	for _, option := range options {
		values[option.Key] = option.Value
	}
	config := &EpayConfig{Merchant: epay.Config{GatewayURL: values["PayAddress"], MerchantID: strings.TrimSpace(values["EpayId"]), Key: values["EpayKey"]}, DisplayType: values["general_setting.quota_display_type"]}
	var err error
	config.Methods, err = operation_setting.ParseEpayMethods(values["PayMethods"])
	if err != nil {
		return nil, err
	}
	config.MinTopUp, err = strconv.ParseInt(values["MinTopUp"], 10, 64)
	if err != nil {
		return nil, errors.New("invalid epay minimum")
	}
	if _, err = operation_setting.EpayMinimumUSD(config.MinTopUp, nil); err != nil {
		return nil, err
	}
	config.Price, err = operation_setting.ParseEpayPrice(values["Price"])
	if err != nil {
		return nil, err
	}
	config.Ratios, err = operation_setting.ParseEpayGroupRatios(values["TopupGroupRatio"])
	if err != nil {
		return nil, err
	}
	if err = common.UnmarshalJsonStr(values["payment_setting.amount_discount"], &config.Discounts); err != nil {
		return nil, errors.New("invalid topup discounts")
	}
	if err = common.UnmarshalJsonStr(values["payment_setting.amount_options"], &config.AmountOptions); err != nil {
		return nil, errors.New("invalid topup presets")
	}
	config.Compliance = values["payment_setting.compliance_confirmed"] == "true" && values["payment_setting.compliance_terms_version"] == operation_setting.CurrentComplianceTermsVersion
	base := values["CustomCallbackAddress"]
	if base == "" {
		base = values["ServerAddress"]
	}
	config.NotifyURL = strings.TrimRight(base, "/") + "/api/user/epay/notify"
	config.ReturnURL = strings.TrimRight(values["ServerAddress"], "/") + "/usage-logs"
	return config, nil
}

func (config *EpayConfig) Enabled() bool {
	_, err := epay.NormalizeGatewayURL(config.Merchant.GatewayURL)
	return config.Compliance && err == nil && config.Merchant.MerchantID != "" && config.Merchant.Key != "" && len(config.Methods) > 0
}

type EpayQuoteRequest struct {
	Amount           int64  `json:"amount"`
	AmountUnit       string `json:"amount_unit"`
	PaymentMethod    string `json:"payment_method"`
	QuoteFingerprint string `json:"quote_fingerprint"`
}

type EpayQuote struct {
	Amount           int64        `json:"amount"`
	AmountUnit       string       `json:"amount_unit"`
	PaymentMethod    string       `json:"payment_method"`
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

func BuildEpayQuote(config *EpayConfig, userID int, group string, request EpayQuoteRequest) (*EpayQuote, error) {
	if !config.Enabled() {
		return nil, &EpayError{"PAYMENT_UNAVAILABLE", "当前管理员未配置支付信息"}
	}
	method := strings.TrimSpace(strings.TrimPrefix(request.PaymentMethod, "epay:"))
	var selected map[string]string
	for _, item := range config.Methods {
		if item["type"] == method {
			selected = item
			break
		}
	}
	if selected == nil {
		return nil, &EpayError{"PAYMENT_UNAVAILABLE", "支付方式不存在"}
	}
	minimum, err := operation_setting.EpayMinimumUSD(config.MinTopUp, selected)
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
		return nil, &EpayError{"VALIDATION_ERROR", "充值数量或单位无效"}
	}
	quota := decimal.NewFromInt(request.Amount)
	if unit == "USD" {
		quota = quota.Mul(decimal.NewFromInt(500000))
	}
	expected, err := common.WalletQuotaFromDecimal(quota)
	if err != nil {
		return nil, &EpayError{"VALIDATION_ERROR", "充值额度超出系统可表示范围"}
	}
	if expected < common.Quota(minimum*500000) {
		return nil, &EpayError{"VALIDATION_ERROR", fmt.Sprintf("充值额度不能小于 %d USD", minimum)}
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
		return nil, errors.New("invalid epay pricing multiplier")
	}
	minor := quota.Mul(decimal.RequireFromString("0.000002")).Mul(config.Price).Mul(ratio).Mul(discount).Round(2).Mul(decimal.NewFromInt(100))
	if !minor.IsPositive() || minor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, &EpayError{"VALIDATION_ERROR", "支付金额超出系统可表示范围"}
	}
	quote := &EpayQuote{Amount: request.Amount, AmountUnit: unit, PaymentMethod: method, AmountMinor: minor.IntPart(), Currency: "CNY", ExpectedQuota: expected, MinTopUp: minimum, UnitPrice: config.Price.String(), GroupRatio: ratio.String(), Discount: discount.String()}
	quote.MinimumQuantity, quote.MinimumQuota = minimum, common.Quota(minimum*500000)
	if unit == "TOKENS" {
		quote.MinimumQuantity = int64(quote.MinimumQuota)
	}
	quote.PayAmount = epay.FormatMoney(quote.AmountMinor)
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

func QuoteEpay(ctx context.Context, userID int, request EpayQuoteRequest) (*EpayQuote, *EpayConfig, error) {
	config, err := LoadEpayConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !config.Enabled() {
		return nil, nil, &EpayError{"PAYMENT_UNAVAILABLE", "当前管理员未配置支付信息"}
	}
	var user model.User
	if err = model.DB.WithContext(ctx).Select("id", "group").First(&user, userID).Error; err != nil {
		return nil, nil, err
	}
	quote, err := BuildEpayQuote(config, userID, user.Group, request)
	if err != nil {
		return nil, nil, err
	}
	if err = model.ValidateTopUpQuotaCapacity(userID, quote.ExpectedQuota); err != nil {
		if errors.Is(err, model.ErrTopUpQuotaLimitExceeded) {
			return nil, nil, &EpayError{"VALIDATION_ERROR", "top-up quota limit exceeded"}
		}
		return nil, nil, err
	}
	return quote, config, nil
}

type EpayCreatedOrder struct {
	Order  *model.TopUp
	Quote  *EpayQuote
	URL    string
	Params map[string]string
}

func CreateEpayTopUp(ctx context.Context, userID int, request EpayQuoteRequest) (*EpayCreatedOrder, error) {
	quote, config, err := QuoteEpay(ctx, userID, request)
	if err != nil {
		var business *EpayError
		if request.QuoteFingerprint != "" && errors.As(err, &business) && (business.Code == "PAYMENT_UNAVAILABLE" || business.Code == "VALIDATION_ERROR") {
			return nil, &EpayError{"QUOTE_CHANGED", "报价已变化，请重新确认"}
		}
		return nil, err
	}
	if request.QuoteFingerprint != "" && !hmac.Equal([]byte(request.QuoteFingerprint), []byte(quote.QuoteFingerprint)) {
		return nil, &EpayError{"QUOTE_CHANGED", "报价已变化，请重新确认"}
	}
	base, err := epay.NormalizeGatewayURL(config.Merchant.GatewayURL)
	if err != nil {
		return nil, err
	}
	config.Merchant.GatewayURL = base
	order := &model.TopUp{UserId: userID, Amount: request.Amount, Money: float64(quote.AmountMinor) / 100, TradeNo: fmt.Sprintf("USR%dNO%s", userID, common.GetRandomString(24)), PaymentMethod: quote.PaymentMethod, PaymentProvider: model.PaymentProviderEpay, CreateTime: time.Now().Unix(), Status: common.TopUpStatusPending}
	if common.AffiliateEnabled && common.AffiliateActivatedAt > 0 {
		base, _ := decimal.NewFromInt(quote.AmountMinor).Mul(decimal.NewFromInt(5000)).QuoRem(config.Price, 0)
		order.AffiliateBaseQuota, err = common.WalletQuotaFromDecimal(base)
		if err != nil || order.AffiliateBaseQuota <= 0 {
			return nil, errors.New("invalid affiliate payment base")
		}
	}
	uri, params, err := epay.BuildPurchase(config.Merchant, epay.Purchase{OrderNo: order.TradeNo, Method: quote.PaymentMethod, Name: fmt.Sprintf("TUC%d", quote.Amount), NotifyURL: config.NotifyURL, ReturnURL: config.ReturnURL, AmountMinor: quote.AmountMinor})
	if err != nil {
		return nil, err
	}
	snapshot := &model.EpayOrder{InputAmount: quote.Amount, AmountUnit: quote.AmountUnit, AmountMinor: quote.AmountMinor, Currency: quote.Currency, ExpectedQuota: quote.ExpectedQuota, UnitPrice: quote.UnitPrice, GroupRatio: quote.GroupRatio, Discount: quote.Discount, QuoteFingerprint: quote.QuoteFingerprint}
	merchant := &model.EpayMerchantVersion{GatewayURL: base, MerchantID: config.Merchant.MerchantID, Key: config.Merchant.Key}
	if err = model.CreateEpayOrder(ctx, order, snapshot, merchant); err != nil {
		return nil, err
	}
	return &EpayCreatedOrder{Order: order, Quote: quote, URL: uri, Params: params}, nil
}
