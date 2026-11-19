package service

import (
	"math"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// PluginBalanceUSD is a balance result in USD for the balance column: USD as it is,
// CNY at the exchange-rate setting. ok is false for another currency, an
// unlimited result or one without an amount.
func PluginBalanceUSD(result jsplugin.BalanceResult) (balance float64, ok bool) {
	if result.Amount == nil || result.Unlimited {
		return 0, false
	}
	switch result.Currency {
	case "USD":
		return *result.Amount, true
	case "CNY":
		rate := operation_setting.USDExchangeRate
		if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return 0, false
		}
		return *result.Amount / rate, true
	}
	return 0, false
}

// UpdatePluginBalance stores a plugin channel's balance result and updates
// the USD balance when the result has one. A failed query keeps the last
// result and records the error.
func UpdatePluginBalance(channelID int, result model.ChannelBalanceDetail) (*model.ChannelBalanceDetail, error) {
	return model.UpdateChannelBalanceDetail(channelID, func(stored *model.ChannelBalanceDetail) (*model.ChannelBalanceDetail, *float64) {
		if result.Error != "" {
			if stored == nil {
				stored = &model.ChannelBalanceDetail{}
			}
			stored.Error = result.Error
			return stored, nil
		}
		if balance, ok := PluginBalanceUSD(result.BalanceResult); ok {
			return &result, &balance
		}
		return &result, nil
	})
}
