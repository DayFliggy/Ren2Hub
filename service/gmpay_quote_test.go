package service

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gmpayQuoteTestConfig mirrors epayQuoteTestConfig with the crypto policy the
// plan froze: a USD unit price of 1, USD-denominated presets, and a concrete
// token.network that the quote must carry into the snapshot.
func gmpayQuoteTestConfig() *GmpayConfig {
	return &GmpayConfig{
		Merchant: gmpay.Config{
			GatewayURL: "https://payments.example.test",
			MerchantID: "1",
			Key:        "test-only-key",
		},
		Assets: []operation_setting.GmpayAsset{
			{Name: "USDT-TRON", Token: "usdt", Network: "tron"},
			{Name: "USDC-ETH", Token: "usdc", Network: "ethereum"},
		},
		Compliance:  true,
		MinTopUp:    1,
		Price:       decimal.NewFromInt(1),
		Ratios:      map[string]decimal.Decimal{"default": decimal.NewFromInt(1)},
		Discounts:   map[int64]decimal.Decimal{},
		DisplayType: operation_setting.QuotaDisplayTypeUSD,
		NotifyURL:   "https://gateway.example.test/api/user/gmpay/notify",
		ReturnURL:   "https://gateway.example.test/usage-logs",
	}
}

// TestBuildGmpayQuotePricesInUSDCents pins the frozen pricing policy: the USD
// base price 1 means one USD is 500000 quota, the gateway is quoted in whole
// cents, and the chain is carried into the quote rather than derived from it.
func TestBuildGmpayQuotePricesInUSDCents(t *testing.T) {
	config := gmpayQuoteTestConfig()
	tests := []struct {
		name        string
		amount      int64
		unit        string
		method      string
		payAmount   string
		amountMinor int64
		quota       common.Quota
	}{
		{"one usd base unit", 1, "USD", "gmpay:usdt.tron", "1.00", 100, 500000},
		{"ten usd base units", 10, "USD", "gmpay:usdt.tron", "10.00", 1000, 5000000},
		{"tokens are quota directly", 500000, "TOKENS", "gmpay:usdt.tron", "1.00", 100, 500000},
		{"second asset is an independent method", 20, "USD", "gmpay:usdc.ethereum", "20.00", 2000, 10000000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quote, err := BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{
				Amount: test.amount, AmountUnit: test.unit, PaymentMethod: test.method,
			})
			require.NoError(t, err)
			assert.Equal(t, test.payAmount, quote.PayAmount)
			assert.Equal(t, test.amountMinor, quote.AmountMinor)
			assert.Equal(t, test.quota, quote.ExpectedQuota)
			assert.Equal(t, "USD", quote.Currency)
			// The quote echoes the bare asset; the prefixed method id is only
			// written to the persisted order.
			assert.Equal(t, strings.TrimPrefix(test.method, "gmpay:"), quote.PaymentMethod)
		})
	}
	quote, err := BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	assert.Equal(t, "usdt", quote.Token)
	assert.Equal(t, "tron", quote.Network)
}

// TestBuildGmpayQuoteAppliesGroupRatioAndUSDDiscount pins that the multipliers
// are applied before the single half-cent rounding, and that presets are keyed
// by USD amount regardless of the display unit.
func TestBuildGmpayQuoteAppliesGroupRatioAndUSDDiscount(t *testing.T) {
	config := gmpayQuoteTestConfig()
	config.Price = decimal.RequireFromString("7.3")
	config.Ratios["vip"] = decimal.RequireFromString("1.2")
	tests := []struct {
		name      string
		group     string
		amount    int64
		unit      string
		payAmount string
		quota     common.Quota
		discount  bool
	}{
		{"default group", "default", 10, "USD", "73.00", 5000000, false},
		{"group ratio applies", "vip", 10, "USD", "87.60", 5000000, false},
		{"usd preset discount applies", "default", 10, "USD", "65.70", 5000000, true},
		{"tokens request uses the same presets", "default", 5000000, "TOKENS", "65.70", 5000000, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Presets are keyed by the whole-USD amount, so a discounted row is
			// expressed by its own config rather than by a table-wide preset.
			candidate := *config
			candidate.Discounts = map[int64]decimal.Decimal{}
			if test.discount {
				candidate.Discounts[10] = decimal.RequireFromString("0.9")
			}
			quote, err := BuildGmpayQuote(&candidate, 1, test.group, GmpayQuoteRequest{
				Amount: test.amount, AmountUnit: test.unit, PaymentMethod: "gmpay:usdt.tron",
			})
			require.NoError(t, err)
			assert.Equal(t, test.payAmount, quote.PayAmount)
			assert.Equal(t, test.quota, quote.ExpectedQuota)
		})
	}
}

// TestBuildGmpayQuoteRoundsHalfCentUp keeps the fiat rounding identical to the
// Epay rail: one half-up rounding at the cent, never a second one.
func TestBuildGmpayQuoteRoundsHalfCentUp(t *testing.T) {
	config := gmpayQuoteTestConfig()
	config.Price = decimal.RequireFromString("1.005")
	quote, err := BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 1, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	assert.Equal(t, "1.01", quote.PayAmount)
	assert.Equal(t, int64(101), quote.AmountMinor)

	config = gmpayQuoteTestConfig()
	config.Price = decimal.RequireFromString("7.333")
	config.Ratios["default"] = decimal.RequireFromString("1.2")
	config.Discounts[7] = decimal.RequireFromString("0.9")
	quote, err = BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 7, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	assert.Equal(t, "55.44", quote.PayAmount)
}

// TestBuildGmpayQuoteEnforcesMinimums covers the global floor and the per-asset
// override: a per-asset minimum only ever raises the floor.
func TestBuildGmpayQuoteEnforcesMinimums(t *testing.T) {
	config := gmpayQuoteTestConfig()
	config.MinTopUp = 10
	_, err := BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 9, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	var business *GmpayError
	require.ErrorAs(t, err, &business)
	assert.Equal(t, "VALIDATION_ERROR", business.Code)
	_, err = BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 4999999, AmountUnit: "TOKENS", PaymentMethod: "gmpay:usdt.tron"})
	require.ErrorAs(t, err, &business)
	quote, err := BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	assert.Equal(t, int64(10), quote.MinTopUp)
	assert.Equal(t, common.Quota(5000000), quote.MinimumQuota)

	config = gmpayQuoteTestConfig()
	config.MinTopUp = 1
	config.Assets[0].MinTopUp = "5"
	quote, err = BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), quote.MinTopUp)
	_, err = BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{Amount: 4, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.ErrorAs(t, err, &business)
}

// TestBuildGmpayQuoteRejectsUnusableInput keeps every rejection a stable client
// code and never a panic on unvalidated amounts.
func TestBuildGmpayQuoteRejectsUnusableInput(t *testing.T) {
	config := gmpayQuoteTestConfig()
	rejected := []struct {
		name    string
		request GmpayQuoteRequest
		code    string
	}{
		{"zero amount", GmpayQuoteRequest{Amount: 0, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"}, "VALIDATION_ERROR"},
		{"negative amount", GmpayQuoteRequest{Amount: -5, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"}, "VALIDATION_ERROR"},
		{"unsupported unit", GmpayQuoteRequest{Amount: 10, AmountUnit: "CNY", PaymentMethod: "gmpay:usdt.tron"}, "VALIDATION_ERROR"},
		{"unlisted network", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.ethereum"}, "PAYMENT_UNAVAILABLE"},
		{"unknown asset", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:dai.polygon"}, "PAYMENT_UNAVAILABLE"},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildGmpayQuote(config, 1, "default", test.request)
			var business *GmpayError
			require.ErrorAs(t, err, &business)
			assert.Equal(t, test.code, business.Code)
		})
	}

	disabled := gmpayQuoteTestConfig()
	disabled.Compliance = false
	_, err := BuildGmpayQuote(disabled, 1, "default", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	var business *GmpayError
	require.ErrorAs(t, err, &business)
	assert.Equal(t, "PAYMENT_UNAVAILABLE", business.Code)

	// The USD amount is multiplied by the fixed 500000 quota unit, so an amount
	// this large cannot be represented as a wallet quota at all.
	overflow := gmpayQuoteTestConfig()
	_, err = BuildGmpayQuote(overflow, 1, "default", GmpayQuoteRequest{Amount: 1 << 50, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.ErrorAs(t, err, &business)
	assert.Equal(t, "VALIDATION_ERROR", business.Code)
}

// TestBuildGmpayQuoteFingerprintBindsPriceGroupAndMerchant proves the
// confirmation token changes whenever anything that set the price changes, so a
// stale browser confirmation cannot be replayed after an administrator edits the
// configuration.
func TestBuildGmpayQuoteFingerprintBindsPriceGroupAndMerchant(t *testing.T) {
	request := GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"}
	baseline, err := BuildGmpayQuote(gmpayQuoteTestConfig(), 1, "default", request)
	require.NoError(t, err)
	for _, mutate := range []func(*GmpayConfig){
		func(c *GmpayConfig) { c.Price = decimal.NewFromInt(2) },
		func(c *GmpayConfig) { c.Ratios["default"] = decimal.NewFromInt(2) },
		func(c *GmpayConfig) { c.Merchant.Key = "rotated-test-key" },
		func(c *GmpayConfig) { c.Merchant.MerchantID = "2" },
		func(c *GmpayConfig) { c.Merchant.GatewayURL = "https://another.example.test" },
		func(c *GmpayConfig) { c.NotifyURL = "https://other.example.test/api/user/gmpay/notify" },
	} {
		config := gmpayQuoteTestConfig()
		mutate(config)
		quote, err := BuildGmpayQuote(config, 1, "default", request)
		require.NoError(t, err)
		assert.NotEqual(t, baseline.QuoteFingerprint, quote.QuoteFingerprint)
	}
	quote, err := BuildGmpayQuote(gmpayQuoteTestConfig(), 1, "vip", request)
	require.NoError(t, err)
	assert.NotEqual(t, baseline.QuoteFingerprint, quote.QuoteFingerprint)
	quote, err = BuildGmpayQuote(gmpayQuoteTestConfig(), 2, "default", request)
	require.NoError(t, err)
	assert.NotEqual(t, baseline.QuoteFingerprint, quote.QuoteFingerprint)
}

// TestGmpayQuoteSerializationHidesCredentials proves the merchant key and the
// derived multipliers never reach a client, while the payable amount and the
// frozen chain do.
func TestGmpayQuoteSerializationHidesCredentials(t *testing.T) {
	quote, err := BuildGmpayQuote(gmpayQuoteTestConfig(), 1, "default", GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"})
	require.NoError(t, err)
	encoded, err := common.Marshal(quote)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "test-only-key")
	assert.NotContains(t, string(encoded), "unit_price")
	assert.NotContains(t, string(encoded), "group_ratio")
	// The fiat amount in cents is serialized as a string so a JavaScript client
	// cannot round it through a float.
	assert.Contains(t, string(encoded), `"amount_minor":"1000"`)
	assert.Contains(t, string(encoded), `"pay_amount":"10.00"`)
	assert.Contains(t, string(encoded), `"currency":"USD"`)
}

// TestParseGmpayAssetsRejectsUncollectableEntries keeps a display-only entry or
// a duplicate chain out of the catalog: every configured asset must be
// collectable and resolve to exactly one frozen token.network.
func TestParseGmpayAssetsRejectsUncollectableEntries(t *testing.T) {
	assets, err := operation_setting.ParseGmpayAssets(`[{"name":"USDT-TRON","token":" USDT ","network":" TRON "}]`)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "usdt", assets[0].Token)
	assert.Equal(t, "tron", assets[0].Network)

	for _, raw := range []string{
		`[{"name":"USDT-TRON","token":"","network":"tron"}]`,
		`[{"name":"USDT-TRON","token":"usdt","network":""}]`,
		`[{"name":"USDT-TRON","token":"usdt tron","network":"tron"}]`,
		`[{"name":"","token":"usdt","network":"tron"}]`,
		`[{"name":"A","token":"usdt","network":"tron"},{"name":"B","token":"usdt","network":"tron"}]`,
		`not-json`,
	} {
		_, err := operation_setting.ParseGmpayAssets(raw)
		require.Error(t, err, raw)
	}
}

// TestSaveGmpayConfigRejectsInvalidInputBeforePublishing proves a rejected save
// leaves no partial write: every candidate value is validated first, so a bad
// price or a bad catalog cannot overwrite a working configuration.
func TestSaveGmpayConfigRejectsInvalidInputBeforePublishing(t *testing.T) {
	validAssets := `[{"name":"USDT-TRON","token":"usdt","network":"tron"}]`
	for name, save := range map[string]func() error{
		"insecure gateway address": func() error {
			return SaveGmpayConfig(nil, "http://payments.example.test", "1", "secret", validAssets, 1, 1, `{"default":1}`)
		},
		"empty merchant id": func() error {
			return SaveGmpayConfig(nil, "https://payments.example.test", "  ", "secret", validAssets, 1, 1, `{"default":1}`)
		},
		"uncollectable asset catalog": func() error {
			return SaveGmpayConfig(nil, "https://payments.example.test", "1", "secret", `[{"name":"USDT","token":"","network":"tron"}]`, 1, 1, `{"default":1}`)
		},
		"non-positive price": func() error {
			return SaveGmpayConfig(nil, "https://payments.example.test", "1", "secret", validAssets, 0, 1, `{"default":1}`)
		},
		"invalid group ratio": func() error {
			return SaveGmpayConfig(nil, "https://payments.example.test", "1", "secret", validAssets, 1, 1, `{"default":0}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, save())
		})
	}
}

func TestGmpayQuoteNumericLimits(t *testing.T) {
	for _, test := range []struct {
		name, price string
		amount      int64
		unit        string
	}{
		{"zero rounded payable", "0.001", 1, "USD"},
		{"gateway minimum", "0.01", 1, "USD"},
		{"payable cents overflow", "10000", math.MaxInt64, "TOKENS"},
		{"wallet quota overflow", "1", math.MaxInt64/500000 + 1, "USD"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := gmpayQuoteTestConfig()
			config.Price = decimal.RequireFromString(test.price)
			_, err := BuildGmpayQuote(config, 1, "default", GmpayQuoteRequest{
				Amount: test.amount, AmountUnit: test.unit, PaymentMethod: "usdt.tron",
			})
			var business *GmpayError
			require.ErrorAs(t, err, &business)
			assert.Equal(t, "VALIDATION_ERROR", business.Code)
		})
	}
	config := gmpayQuoteTestConfig()
	config.MinTopUp = math.MaxInt64 / 500000
	request := GmpayQuoteRequest{Amount: config.MinTopUp, AmountUnit: "USD", PaymentMethod: "usdt.tron"}
	quote, err := BuildGmpayQuote(config, 1, "default", request)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(config.MinTopUp*500000), quote.MinimumQuota)
	assert.Equal(t, quote.MinimumQuota, quote.ExpectedQuota)
	assert.Equal(t, config.MinTopUp*100, quote.AmountMinor)
	config.MinTopUp++
	_, err = BuildGmpayQuote(config, 1, "default", request)
	require.Error(t, err, "minimum quota multiplication must never overflow")
	config.MinTopUp = 1
	config.Assets[0].MinTopUp = strconv.FormatInt(math.MaxInt64/500000+1, 10)
	_, err = BuildGmpayQuote(config, 1, "default", request)
	require.Error(t, err, "per-asset minimum uses the same bound")
}
