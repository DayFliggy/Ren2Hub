package billing_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/require"
)

func TestUsageUnitsNeverExchangeDifferentDimensions(t *testing.T) {
	value, err := billing.ConvertUsageUnit("1.25", "minute", "second")
	require.NoError(t, err)
	require.Equal(t, "75", value)
	value, err = billing.ConvertUsageUnit("1", "hour", "second")
	require.NoError(t, err)
	require.Equal(t, "3600", value)
	for _, pair := range [][2]string{{"character", "token"}, {"credit", "second"}, {"image", "count"}} {
		_, err = billing.ConvertUsageUnit("1", pair[0], pair[1])
		require.Error(t, err)
	}
}

func TestAudioMeteringFreezesCharacterCountAndPricesTimeUnits(t *testing.T) {
	version := billing.PriceVersion{Model: "speech", Mode: billing.ModeUnitPrice,
		Metering: &billing.PriceMetering{RequestKind: "speech", Quantity: "characters", Source: "request_fixed", Method: "unicode_codepoints"},
		Prices:   []billing.UnitPrice{{Quantity: "characters", Unit: "per_million_characters", Price: "1"}}}
	quote, err := billing.NewQuote(version, "1", map[string]any{"quantity": "3"}, time.Now())
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{Complete: true, Quantities: map[string]string{"characters": "3"}})
	require.NoError(t, err)
	require.Equal(t, "0.000003", charge.TotalUSD)
	require.EqualValues(t, 2, charge.FinalQuota)
	for _, facts := range []map[string]string{nil, {"characters": "2"}, {"characters": "2.5"}} {
		_, err = billing.Calculate(quote, billing.Usage{Complete: true, Quantities: facts})
		require.Error(t, err)
	}
	version.Metering = nil
	_, err = billing.PreparePriceVersion(version)
	require.ErrorContains(t, err, "missing_usage_contract")
	version = billing.PriceVersion{Model: "stt", Mode: billing.ModeUnitPrice, Metering: &billing.PriceMetering{RequestKind: "transcription", Quantity: "seconds", Source: "completion_fact", Method: "provider_duration", ReservationQuantity: "60"}, Prices: []billing.UnitPrice{{Quantity: "seconds", Unit: "per_minute", Price: "0.06"}}}
	quote, err = billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	charge, err = billing.Calculate(quote, billing.Usage{Complete: true, Quantities: map[string]string{"seconds": "1.5"}})
	require.NoError(t, err)
	require.Equal(t, "0.0015", charge.TotalUSD)
}
