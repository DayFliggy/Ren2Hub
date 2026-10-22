package operation_setting

import (
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

// Prices, balances and quota displays divide or multiply by these options, so a
// stored zero, negative or non-finite value corrupts every amount shown or
// charged. The runtime parsers ignore errors, so invalid values must stop here.
var currencyOptionMessages = map[string]string{
	"USDExchangeRate": "Exchange rate must be greater than 0",
	"general_setting.custom_currency_exchange_rate": "Exchange rate must be greater than 0",
	"QuotaPerUnit": "Quota per unit must be greater than 0",
}

// ValidateCurrencyOption requires currency conversion options to be finite
// numbers greater than zero. Other keys are not checked.
func ValidateCurrencyOption(key, value string) error {
	message, ok := currencyOptionMessages[key]
	if !ok {
		return nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
		return common.NewMessage(message)
	}
	return nil
}
