package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCurrencyOptionRequiresFinitePositiveNumbers(t *testing.T) {
	keys := map[string]string{
		"USDExchangeRate": "Exchange rate must be greater than 0",
		"general_setting.custom_currency_exchange_rate": "Exchange rate must be greater than 0",
		"QuotaPerUnit": "Quota per unit must be greater than 0",
	}
	for key, message := range keys {
		for _, value := range []string{"0", "-1", "-0.5", "", "abc", "NaN", "Inf", "-Inf", "1e400"} {
			err := ValidateCurrencyOption(key, value)
			var msg *common.Message
			require.ErrorAs(t, err, &msg, "%s=%q", key, value)
			assert.Equal(t, message, msg.Key, "%s=%q", key, value)
		}
		for _, value := range []string{"0.0001", "1", "7.3", "500000"} {
			assert.NoError(t, ValidateCurrencyOption(key, value), "%s=%q", key, value)
		}
	}
}

func TestValidateCurrencyOptionIgnoresOtherKeys(t *testing.T) {
	assert.NoError(t, ValidateCurrencyOption("Price", "0"))
	assert.NoError(t, ValidateCurrencyOption("general_setting.quota_display_type", "abc"))
}
