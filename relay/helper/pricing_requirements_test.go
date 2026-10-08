package helper

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPricingRequirementsRejectMissingSearchBeforeReservation(t *testing.T) {
	for _, name := range []string{"gpt-4o-mini-search-preview", "gpt-4o-search-preview"} {
		t.Run(name, func(t *testing.T) {
			c, info := pricingFixture(t, name, model.PricingValues{"ModelRatio": "0.5", "CompletionRatio": "2"})
			_, err := ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
			require.ErrorContains(t, err, "missing_price_dimension")
			require.Equal(t, 503, BillingPriceAPIError(err).StatusCode)
			require.Nil(t, info.Billing)
			require.Nil(t, info.PriceData.Quote)
		})
	}
}

func TestPricingRequirementsUseFinalTierAndEnabledTools(t *testing.T) {
	c, info := pricingFixture(t, "model", model.PricingValues{"ModelRatio": "0.5", "CompletionRatio": "2"})
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
	require.NoError(t, err, "explicit manual pricing applies without a source tier allowlist")
	err = FreezeOutboundBillingAttributes(info, []byte(`{"service_tier":3}`))
	require.Error(t, err)
	require.Equal(t, 400, BillingRequestAPIError(err).StatusCode)
}

func TestGoogleMapsRequiresItsOwnUsageContract(t *testing.T) {
	_, info := pricingFixture(t, "model", model.PricingValues{"ModelPrice": "0"})
	for _, body := range []string{`{"tools":[{"googleMaps":{}}]}`, `{"tools":[{"google_maps":{}}]}`} {
		_, err := PricingRequirementsFromBody(info, []byte(body))
		require.ErrorContains(t, err, "missing_usage_contract")
		require.ErrorContains(t, err, "tool:google_maps")
	}
}

// useToolPrices installs operator tool prices (tool_price_setting.prices) for
// one test and restores the empty default afterwards.
func useToolPrices(t *testing.T, prices string) {
	t.Helper()
	require.NoError(t, operation_setting.LoadToolPricesFromJSONString(prices))
	t.Cleanup(func() { require.NoError(t, operation_setting.LoadToolPricesFromJSONString(`{}`)) })
}

// Anthropic bills web_fetch, code_execution and tool_search from
// usage.server_tool_use, so a Claude wire request declaring one is refused
// before reservation until the operator prices it; an explicit zero is a price.
func TestClaudeServerToolsRequireTheirOwnPrice(t *testing.T) {
	for _, tool := range []struct{ kind, key string }{
		{"web_fetch_20250910", "web_fetch"},
		{"code_execution_20250825", "code_execution"},
		{"tool_search_tool_regex_20251119", "tool_search"},
	} {
		t.Run(tool.key, func(t *testing.T) {
			c, info := pricingFixture(t, "claude-test", model.PricingValues{"ModelRatio": "0.5", "CompletionRatio": "2"})
			info.RelayFormat = types.RelayFormatClaude
			body := []byte(`{"tools":[{"type":"` + tool.kind + `","name":"` + tool.key + `"}]}`)
			require.NoError(t, FreezeOutboundBillingAttributes(info, body))
			_, err := ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
			require.ErrorContains(t, err, `missing_price_dimension: dimension "tool:`+tool.key+`"`)
			require.Nil(t, info.PriceData.Quote)

			useToolPrices(t, `{"`+tool.key+`":0}`)
			_, err = ModelPriceHelper(c, info, 10, &types.TokenCountMeta{MaxTokens: 5})
			require.NoError(t, err)

			// The OpenAI hosted code tool keeps its own refusal.
			info.RelayFormat = types.RelayFormatOpenAIResponses
			_, err = PricingRequirementsFromBody(info, []byte(`{"tools":[{"type":"code_interpreter"}]}`))
			require.ErrorContains(t, err, "missing_usage_contract")
		})
	}
}

// A channel that bills web search under its own key requires that key, not
// the protocol web_search key it never bills.
func TestWebSearchBillingKeyReplacesProtocolRequirement(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}
	body := []byte(`{"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)
	required, err := PricingRequirementsFromBody(info, body)
	require.NoError(t, err)
	require.Equal(t, []string{"tool:web_search"}, required.Dimensions)

	info.WebSearchBillingKey = "search_strategy_agent"
	info.VendorToolPriceKeys = map[string][]string{"web_search": {"search_strategy_agent"}}
	required, err = PricingRequirementsFromBody(info, body)
	require.NoError(t, err)
	require.Equal(t, []string{"tool:search_strategy_agent"}, required.Dimensions)
}

func TestRealtimeFixedRequestPriceQualifiesWithoutSeparateTokenPrices(t *testing.T) {
	for _, version := range []model.PricingValues{{"ModelPrice": "0.000006"}} {
		c, info := pricingFixture(t, "realtime", version)
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
	c, info := pricingFixture(t, "realtime", model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p+c+ai+ao+img"})
	info.RelayFormat = types.RelayFormatOpenAIRealtime
	_, err := ModelPriceHelper(c, info, 1, nil)
	require.NoError(t, err)
	require.NoError(t, ValidateRealtimeBillingEvent(info, []byte(`{"type":"conversation.item.create","item":{"content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}}`)))
}
