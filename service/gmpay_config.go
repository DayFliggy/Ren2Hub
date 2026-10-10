package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm/clause"
)

// GmpayError mirrors EpayError: a stable code for the client and a web console
// message that never carries merchant credentials.
type GmpayError struct {
	Code    string
	Message *common.Message
}

func (err *GmpayError) Error() string { return err.Message.Error() }

// Unwrap exposes the message, so error responses carry its message_key.
func (err *GmpayError) Unwrap() error { return err.Message }

// GmpayConfig is the GMPay checkout configuration. It is loaded from one SELECT
// so a bulk credential rotation cannot be observed half-applied.
type GmpayConfig struct {
	Merchant      gmpay.Config `json:"-"`
	Assets        []operation_setting.GmpayAsset
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

// GmpayMethodID is the payment-method identifier a client submits for one frozen
// asset. It carries the concrete token and network, never a chain-agnostic name.
func GmpayMethodID(asset operation_setting.GmpayAsset) string {
	return "gmpay:" + asset.Token + "." + asset.Network
}

// GmpayMethods renders the configured assets as payment methods.
func (config *GmpayConfig) GmpayMethods() ([]map[string]string, error) {
	methods := make([]map[string]string, 0, len(config.Assets))
	for _, asset := range config.Assets {
		minimum, err := operation_setting.GmpayMinimumUSD(config.MinTopUp, asset)
		if err != nil {
			return nil, err
		}
		methods = append(methods, map[string]string{
			"name": asset.Name, "type": GmpayMethodID(asset), "token": asset.Token,
			"network": asset.Network, "icon": asset.Icon, "color": asset.Color,
			"min_topup": strconv.FormatInt(minimum, 10),
		})
	}
	return methods, nil
}

// FindAsset resolves one submitted method identifier back to its frozen asset.
func (config *GmpayConfig) FindAsset(methodID string) (operation_setting.GmpayAsset, bool) {
	value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(methodID), "gmpay:")))
	for _, asset := range config.Assets {
		if asset.Token+"."+asset.Network == value {
			return asset, true
		}
	}
	return operation_setting.GmpayAsset{}, false
}

// LoadGmpayConfig reads the whole GMPay surface in one query. Defaults are the
// neutral values a fresh installation would show before any operator input.
func LoadGmpayConfig(ctx context.Context) (*GmpayConfig, error) {
	values := map[string]string{
		"GmpayPayAddress": "", "GmpayId": "", "GmpayKey": "", "GmpayAssets": "[]",
		"CustomCallbackAddress": "", "ServerAddress": "http://localhost:3000",
		"GmpayPrice": "1", "GmpayMinTopUp": "1", "GmpayGroupRatio": `{"default":1}`,
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

	config := &GmpayConfig{
		Merchant:    gmpay.Config{GatewayURL: values["GmpayPayAddress"], MerchantID: strings.TrimSpace(values["GmpayId"]), Key: values["GmpayKey"]},
		DisplayType: values["general_setting.quota_display_type"],
	}
	var err error
	if config.Assets, err = operation_setting.ParseGmpayAssets(values["GmpayAssets"]); err != nil {
		return nil, err
	}
	if config.MinTopUp, err = strconv.ParseInt(values["GmpayMinTopUp"], 10, 64); err != nil {
		return nil, errors.New("invalid gmpay minimum")
	}
	if _, err = operation_setting.GmpayMinimumUSD(config.MinTopUp, operation_setting.GmpayAsset{}); err != nil {
		return nil, err
	}
	if config.Price, err = operation_setting.ParseEpayPrice(values["GmpayPrice"]); err != nil {
		return nil, errors.New("invalid gmpay price")
	}
	if config.Ratios, err = operation_setting.ParseEpayGroupRatios(values["GmpayGroupRatio"]); err != nil {
		return nil, err
	}
	if err = common.UnmarshalJsonStr(values["payment_setting.amount_discount"], &config.Discounts); err != nil {
		return nil, errors.New("invalid topup discounts")
	}
	if err = common.UnmarshalJsonStr(values["payment_setting.amount_options"], &config.AmountOptions); err != nil {
		return nil, errors.New("invalid topup presets")
	}
	config.Compliance = values["payment_setting.compliance_confirmed"] == "true" &&
		values["payment_setting.compliance_terms_version"] == operation_setting.CurrentComplianceTermsVersion
	// A disabled rail cannot create an order and does not require public URLs.
	if !config.Enabled() {
		return config, nil
	}
	config.NotifyURL, config.ReturnURL, err = paymentCallbackURLs(values["ServerAddress"], values["CustomCallbackAddress"], "/api/user/gmpay/notify")
	if err != nil {
		return nil, err
	}
	return config, nil
}

// Enabled requires a usable gateway credential and at least one frozen asset.
// The asset catalog is part of the contract: without it there is nothing to
// collect. The selected network is recorded as the requested network only.
func (config *GmpayConfig) Enabled() bool {
	_, err := gmpay.NormalizeGatewayURL(config.Merchant.GatewayURL)
	return config.Compliance && err == nil && config.Merchant.MerchantID != "" && config.Merchant.Key != "" && len(config.Assets) > 0
}

// GMPay requests have a bounded lifetime and never forward signed forms through redirects.
func gmpayHTTPClient() *http.Client {
	client := *GetHttpClient()
	if client.Timeout <= 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

// GmpayGatewayIsReachable fetches the gateway's public configuration. The
// endpoint carries no signature, so a success proves reachability and the
// supported asset catalog, not the merchant credential: the only
// credential-checking route is order creation, which has side effects and is
// therefore never used as a probe.
func GmpayGatewayIsReachable(ctx context.Context, payAddress string) ([]gmpay.SupportedAsset, error) {
	base, err := gmpay.NormalizeGatewayURL(payAddress)
	if err != nil {
		return nil, err
	}
	endpoint, err := gmpay.PublicConfigEndpoint(gmpay.Config{GatewayURL: base})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := gmpayHTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, gmpay.ErrGatewayRejected
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		return nil, gmpay.ErrGatewayRejected
	}
	return gmpay.ParsePublicConfig(body)
}

// SaveGmpayConfig publishes the whole GMPay surface at once. An empty secret
// keeps the stored one, so an administrator returning to the page does not have
// to re-paste a value that is stripped from the option read.
func SaveGmpayConfig(ctx context.Context, payAddress, merchantID, secretKey, assets string, price float64, minTopUp int, groupRatio string) error {
	base, err := gmpay.NormalizeGatewayURL(payAddress)
	if err != nil {
		return err
	}
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" || len(merchantID) > 128 {
		return errors.New("invalid gmpay merchant id")
	}
	parsedAssets, err := operation_setting.ParseGmpayAssets(assets)
	if err != nil {
		return err
	}
	priceText := strconv.FormatFloat(price, 'f', -1, 64)
	if _, err = operation_setting.ParseEpayPrice(priceText); err != nil {
		return errors.New("invalid gmpay price")
	}
	if _, err = operation_setting.GmpayMinimumUSD(int64(minTopUp), operation_setting.GmpayAsset{}); err != nil {
		return err
	}
	if _, err = operation_setting.ParseEpayGroupRatios(groupRatio); err != nil {
		return err
	}
	encodedAssets, err := common.Marshal(parsedAssets)
	if err != nil {
		return err
	}
	values := map[string]string{
		"GmpayPayAddress": base,
		"GmpayId":         merchantID,
		"GmpayAssets":     string(encodedAssets),
		"GmpayPrice":      priceText,
		"GmpayMinTopUp":   strconv.Itoa(minTopUp),
		"GmpayGroupRatio": groupRatio,
	}
	if secret := strings.TrimSpace(secretKey); secret != "" {
		values["GmpayKey"] = secret
	}
	if err = model.UpdateOptionsBulk(values); err != nil {
		return err
	}
	return nil
}
