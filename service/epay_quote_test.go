package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func epayQuoteTestConfig() *EpayConfig {
	return &EpayConfig{
		Merchant:   epay.Config{GatewayURL: "https://payments.example.test", MerchantID: "1", Key: "test-only-key"},
		Methods:    []map[string]string{{"name": "Alipay", "type": "alipay"}},
		Compliance: true, MinTopUp: 1, Price: decimal.RequireFromString("7.3"),
		Ratios: map[string]decimal.Decimal{"default": decimal.NewFromInt(1)},
	}
}

func TestEpayQuoteExactPricing(t *testing.T) {
	for _, tc := range []struct {
		name, unit, price, ratio, discount, money string
		amount, quota                             int64
	}{
		{"USD", "USD", "7.3", "1", "1", "73.00", 10, 5000000},
		{"fractional USD tokens", "TOKENS", "7.3", "1", "1", "10.95", 750000, 750000},
		{"round half cent up", "USD", "1.005", "1", "1", "1.01", 1, 500000},
		{"group and discount before rounding", "USD", "7.333", "1.2", "0.9", "7.92", 1, 500000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := epayQuoteTestConfig()
			config.Price = decimal.RequireFromString(tc.price)
			config.Ratios["default"] = decimal.RequireFromString(tc.ratio)
			config.Discounts = map[int64]decimal.Decimal{tc.amount: decimal.RequireFromString(tc.discount)}
			quote, err := BuildEpayQuote(config, 1, "default", EpayQuoteRequest{Amount: tc.amount, AmountUnit: tc.unit, PaymentMethod: "alipay"})
			require.NoError(t, err)
			assert.Equal(t, tc.money, quote.PayAmount)
			assert.Equal(t, common.Quota(tc.quota), quote.ExpectedQuota)
			encoded, err := common.Marshal(quote)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"amount_minor":"`)
			assert.Contains(t, string(encoded), `"expected_quota":"`)
			assert.NotContains(t, string(encoded), config.Merchant.Key)
		})
	}
}

func TestEpayQuoteMinimumAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name, unit, methodMinimum string
		globalMinimum, amount     int64
		wantError                 bool
	}{
		{"global dominates", "USD", "2", 5, 4, true},
		{"method dominates", "USD", "5", 2, 4, true},
		{"equal minimum", "USD", "5", 2, 5, false},
		{"tokens below USD minimum", "TOKENS", "2", 1, 999999, true},
		{"tokens minimum exact", "TOKENS", "2", 1, 1000000, false},
		{"bad method config", "USD", "1.5", 1, 10, true},
		{"bad global config", "USD", "1", math.MaxInt64, 10, true},
		{"quota overflow", "USD", "1", 1, math.MaxInt64, true},
		{"invalid unit", "CNY", "1", 1, 10, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := epayQuoteTestConfig()
			config.MinTopUp = tc.globalMinimum
			config.Methods[0]["min_topup"] = tc.methodMinimum
			_, err := BuildEpayQuote(config, 1, "default", EpayQuoteRequest{Amount: tc.amount, AmountUnit: tc.unit, PaymentMethod: "alipay"})
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
	config := epayQuoteTestConfig()
	config.Price = decimal.NewFromInt(math.MaxInt64)
	_, err := BuildEpayQuote(config, 1, "default", EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.Error(t, err)
}

func TestEpayDiscountUsesUSDForBothInputUnits(t *testing.T) {
	config := epayQuoteTestConfig()
	config.Discounts = map[int64]decimal.Decimal{10: decimal.RequireFromString("0.9"), 500000: decimal.RequireFromString("0.1")}
	usd, err := BuildEpayQuote(config, 1, "default", EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.NoError(t, err)
	tokens, err := BuildEpayQuote(config, 1, "default", EpayQuoteRequest{Amount: 5000000, AmountUnit: "TOKENS", PaymentMethod: "alipay"})
	require.NoError(t, err)
	assert.Equal(t, "65.70", usd.PayAmount)
	assert.Equal(t, usd.PayAmount, tokens.PayAmount)
	small, err := BuildEpayQuote(config, 1, "default", EpayQuoteRequest{Amount: 500000, AmountUnit: "TOKENS", PaymentMethod: "alipay"})
	require.NoError(t, err)
	assert.Equal(t, "7.30", small.PayAmount)
}

func TestEpayQuoteFingerprintBindsPriceGroupAndMerchant(t *testing.T) {
	request := EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"}
	baseline, err := BuildEpayQuote(epayQuoteTestConfig(), 1, "default", request)
	require.NoError(t, err)
	for _, mutate := range []func(*EpayConfig){
		func(c *EpayConfig) { c.Price = decimal.NewFromInt(8) },
		func(c *EpayConfig) { c.Ratios["default"] = decimal.NewFromInt(2) },
		func(c *EpayConfig) { c.Merchant.Key = "rotated-test-key" },
		func(c *EpayConfig) { c.Merchant.MerchantID = "2" },
		func(c *EpayConfig) { c.Merchant.GatewayURL = "https://another.example.test" },
	} {
		config := epayQuoteTestConfig()
		mutate(config)
		quote, err := BuildEpayQuote(config, 1, "default", request)
		require.NoError(t, err)
		assert.NotEqual(t, baseline.QuoteFingerprint, quote.QuoteFingerprint)
	}
	quote, err := BuildEpayQuote(epayQuoteTestConfig(), 1, "vip", request)
	require.NoError(t, err)
	assert.NotEqual(t, baseline.QuoteFingerprint, quote.QuoteFingerprint)
}
