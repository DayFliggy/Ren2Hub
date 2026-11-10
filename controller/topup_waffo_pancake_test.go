package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestRoundTopUpPayUsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   string
		expected string
	}{
		{name: "whole amount", amount: "29", expected: "29.00"},
		{name: "decimal amount", amount: "29.9", expected: "29.90"},
		{name: "round half up to cents", amount: "29.995", expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, service.RoundTopUpPay(decimal.RequireFromString(tc.amount), "USD"))
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := setting.WaffoPancakeUnitPrice
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		setting.WaffoPancakeUnitPrice = originalUnitPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
	})
	setting.WaffoPancakeUnitPrice = 2.5

	testCases := []struct {
		name             string
		amount           int64
		quotaDisplayType string
		expected         string
	}{
		{name: "currency display applies unit price", amount: 10, quotaDisplayType: operation_setting.QuotaDisplayTypeUSD, expected: "25"},
		{name: "tokens display converts quota to display units before pricing", amount: int64(common.QuotaPerUnit * 3), quotaDisplayType: operation_setting.QuotaDisplayTypeTokens, expected: "7.5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			require.Equal(t, tc.expected, getWaffoPancakePayMoney(tc.amount).String())
		})
	}
}

func TestGetWaffoPayMoneyStaysDecimalUnderTokens(t *testing.T) {
	originalUnitPrice, originalQuotaPerUnit := setting.WaffoUnitPrice, common.QuotaPerUnit
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() {
		setting.WaffoUnitPrice, common.QuotaPerUnit = originalUnitPrice, originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
	})
	setting.WaffoUnitPrice, common.QuotaPerUnit = 0.01, 1.1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	// 33 / 1.1 is exactly 30; float division gives 29.999999999999996, which
	// after a 5% discount rounds 0.285 down to 0.28 instead of up to 0.29.
	list := getWaffoPayMoney(33)
	require.Equal(t, "0.3", list.String())
	require.Equal(t, "0.29", service.RoundTopUpPay(list.Mul(decimal.RequireFromString("0.95")), "USD"))
}
