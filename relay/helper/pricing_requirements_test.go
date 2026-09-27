package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestPricingRequirementsRejectMissingSearchBeforeReservation(t *testing.T) {
	for _, name := range []string{"gpt-4o-mini-search-preview", "gpt-4o-search-preview"} {
		t.Run(name, func(t *testing.T) {
			c, info := pricingFixture(t, billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}, {Quantity: "c", Unit: "per_million_tokens", Price: "2"}}})
			_, err := ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
			require.ErrorContains(t, err, "missing_price_dimension")
			require.Equal(t, 503, BillingPriceAPIError(err).StatusCode)
			require.Nil(t, info.Billing)
			require.Nil(t, info.PriceData.Quote)
		})
	}
}

func TestPricingRequirementsUseFinalTierAndEnabledTools(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "model", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}, {Quantity: "c", Unit: "per_million_tokens", Price: "2"}}})
	info.BillingRequestInput.Attributes["service_tier"] = "priority"
	_, err := QualifyPublishedPrice(c, info)
	require.NoError(t, err)
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"service_tier":"default"}`)))
	_, err = ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
	require.NoError(t, err)
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"tools":[{"type":"web_search"}]}`)))
	_, err = ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
	require.ErrorContains(t, err, "missing_price_dimension")
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"service_tier":"priority"}`)))
	_, err = ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
	require.ErrorContains(t, err, "unverified_service_tier")
	err = FreezeOutboundBillingAttributes(info, []byte(`{"service_tier":3}`))
	require.Error(t, err)
	require.Equal(t, 400, BillingRequestAPIError(err).StatusCode)
}

func TestGoogleMapsRequiresItsOwnUsageContract(t *testing.T) {
	_, info := pricingFixture(t, billing.PriceVersion{Model: "model", Mode: billing.ModeFree})
	for _, body := range []string{`{"tools":[{"googleMaps":{}}]}`, `{"tools":[{"google_maps":{}}]}`} {
		_, err := PricingRequirementsFromBody(info, []byte(body))
		require.ErrorContains(t, err, "missing_usage_contract")
		require.ErrorContains(t, err, "tool:google_maps")
	}
}

func TestRealtimeFixedRequestPriceQualifiesWithoutSeparateTokenPrices(t *testing.T) {
	for _, version := range []billing.PriceVersion{
		{Model: "realtime", Mode: billing.ModeUnitPrice, FlatFee: "0.000006"},
		{Model: "realtime", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.000006"}}},
	} {
		c, info := pricingFixture(t, version)
		info.RelayFormat = types.RelayFormatOpenAIRealtime
		price, err := ModelPriceHelper(c, info, 10, nil)
		require.NoError(t, err)
		require.Equal(t, 3, price.QuotaToPreConsume)
		require.NoError(t, ValidateRealtimeBillingEvent(info, []byte(`{"type":"session.update","session":{"tools":[{"type":"function","name":"client_search"}]}}`)))
		for _, body := range []string{
			`{"type":"session.update","session":{"input_audio_transcription":{"model":"whisper-1"}}}`,
			`{"type":"session.update","session":{"audio":{"input":{"transcription":{"model":"whisper-1"}}}}}`,
			`{"type":"response.create","response":{"audio":{"input":{"transcription":{"model":"whisper-1"}}}}}`,
			`{"type":"session.update","session":{"tools":[{"type":"web_search"}]}}`,
			`{"type":"session.update","session":{"tools":[{"type":"unregistered_tool"}]}}`,
		} {
			err := ValidateRealtimeBillingEvent(info, []byte(body))
			require.ErrorContains(t, err, "missing_usage_contract", body)
		}
		require.NoError(t, ValidateRealtimeBillingEvent(info, []byte(`{"type":"session.update","session":{"audio":{"input":{"transcription":null}}}}`)))
	}
}

func TestRealtimeImageInputUsesRegisteredPriceDimension(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "realtime", Mode: billing.ModeExpression, Expression: "p+c+ai+ao+img"})
	info.RelayFormat = types.RelayFormatOpenAIRealtime
	_, err := ModelPriceHelper(c, info, 1, nil)
	require.NoError(t, err)
	require.NoError(t, ValidateRealtimeBillingEvent(info, []byte(`{"type":"conversation.item.create","item":{"content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}}`)))
}
