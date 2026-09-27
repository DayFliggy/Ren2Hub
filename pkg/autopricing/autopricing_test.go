package autopricing_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceUnitsAndExplicitZero(t *testing.T) {
	for _, test := range []struct {
		name   string
		source autopricing.SourceID
		data   string
		model  string
	}{
		{"sub", autopricing.SourceSub2API, `{"m":{"input_cost_per_token":0.000000123456789123456789,"output_cost_per_token":0,"cache_read_input_token_cost":0}}`, "m"},
		{"axon", autopricing.SourceAxonHub, `{"providers":{"p":{"models":[{"id":"m","cost":{"input":0.123456789123456789,"output":0,"cache_read":0}}]}}}`, "m"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, rejected, err := autopricing.ParseWithReport(test.source, []byte(test.data))
			require.NoError(t, err)
			require.Empty(t, rejected)
			entry, ok := entries[test.model]
			require.True(t, ok)
			assert.Equal(t, billing.ModeExpression, entry.Price.Mode)
			assert.Contains(t, entry.Price.Expression, "p * 0.123456789123456789 + c * 0 + cr * 0")
			assert.Empty(t, entry.Price.Prices)
			require.NotNil(t, entry.Price.Provenance)
			assert.Equal(t, autopricing.ParserVersion, entry.Price.Provenance.ParserVersion)
			require.Len(t, entry.Price.Provenance.Fields, 3)
			for _, field := range entry.Price.Provenance.Fields {
				assert.NotEmpty(t, field.Target)
				assert.NotEmpty(t, field.RawValue)
				assert.Contains(t, field.SourcePath, "/")
				if test.name == "sub" {
					assert.Equal(t, "usd_per_token", field.SourceUnit)
					assert.Equal(t, "multiply_by_1000000", field.Conversion)
				} else {
					assert.Equal(t, "usd_per_million_tokens", field.SourceUnit)
					assert.Equal(t, "identity", field.Conversion)
				}
			}
			require.NoError(t, billing.ValidatePriceVersion(entry.Price))
			quote, err := billing.NewQuote(entry.Price, "1", map[string]interface{}{"service_tier": "default"}, time.Unix(1, 0))
			require.NoError(t, err)
			charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, OutputTokens: 100, CacheReadTokens: 100, InputIncludesCache: true})
			require.NoError(t, err)
			assert.Equal(t, "0", charge.TotalUSD)
		})
	}
}

func TestMissingUnsafeAndMetadataAreDistinct(t *testing.T) {
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(`{
	 "valid":{"input_cost_per_token":0,"output_cost_per_token":0,"supports_service_tier":false,"supports_prompt_caching":false},
	 "missing":{"input_cost_per_token":0.000001},
	 "negative":{"input_cost_per_token":-1,"output_cost_per_token":0},
	 "unsupported":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_video_per_second":0.1},
	 "invalid":{"input_cost_per_token":"NaN","output_cost_per_token":0},
	 "null":{"input_cost_per_token":null,"output_cost_per_token":0}
	}`))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Contains(t, entries, "valid")
	require.Len(t, rejected, 5)
	for _, r := range rejected {
		assert.Equal(t, r.Model != "missing", r.BlockFallback, r.Reason)
	}
	_, _, err = autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(`{"m":{"input_cost_per_token":0}}`))
	require.Error(t, err)
	_, err = autopricing.Parse(autopricing.SourceID("unknown"), []byte(`{}`))
	require.Error(t, err)
	_, err = autopricing.Parse(autopricing.SourceSub2API, []byte(`null`))
	require.Error(t, err)
}

func TestMalformedIdentifiedModelDoesNotDiscardValidCatalogEntries(t *testing.T) {
	for _, test := range []struct {
		source autopricing.SourceID
		data   string
	}{
		{autopricing.SourceSub2API, `{"valid":{"input_cost_per_token":0,"output_cost_per_token":0},"primitive":42,"null":null,"bad-cost":{"input_cost_per_token":{},"output_cost_per_token":0}}`},
		{autopricing.SourceAxonHub, `{"p":{"models":{"valid":{"cost":{"input":0,"output":0}},"primitive":42,"null":null,"bad-cost":{"cost":false},"bad-id":{"id":42,"cost":{"input":0,"output":0}}}}}`},
	} {
		t.Run(string(test.source), func(t *testing.T) {
			entries, rejected, err := autopricing.ParseWithReport(test.source, []byte(test.data))
			require.NoError(t, err)
			require.Contains(t, entries, "valid")
			for _, model := range []string{"primitive", "null", "bad-cost"} {
				assert.NotContains(t, entries, model)
				found := false
				for _, rejection := range rejected {
					if rejection.Model == model {
						found = true
						assert.NotEmpty(t, rejection.Reason)
					}
				}
				assert.True(t, found, model)
			}
		})
	}
}

func TestContextBoundaryAndServiceTierNormalization(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{"m":{
	 "input_cost_per_token":0.000001,"output_cost_per_token":0.000002,
	 "input_cost_per_token_above_200k_tokens":0.000003,"output_cost_per_token_above_200k_tokens":0.000004,
	 "cache_read_input_token_cost_above_200k_tokens":0.0000005,"cache_read_input_token_cost":0.000001,
	 "input_cost_per_token_priority":0.000002,"output_cost_per_token_priority":0.000004,
	 "input_cost_per_token_above_200k_tokens_priority":0.000006,"output_cost_per_token_above_200k_tokens_priority":0.000008,
	 "cache_read_input_token_cost_above_200k_tokens_priority":0.000001,"cache_read_input_token_cost_priority":0.000002,
	 "input_cost_per_token_flex":0.0000005,"output_cost_per_token_flex":0.000001,
	 "input_cost_per_token_above_200k_tokens_flex":0.0000015,"output_cost_per_token_above_200k_tokens_flex":0.000002,
	 "cache_read_input_token_cost_above_200k_tokens_flex":0.00000025,"cache_read_input_token_cost_flex":0.0000005,
	 "input_cost_per_token_batches":-1
	}}`))
	require.NoError(t, err)
	for _, test := range []struct {
		context   int64
		tier, usd string
	}{
		{200000, "", "0.0001"}, {200001, "", "0.000175"}, {200000, "priority", "0.0002"}, {200001, "priority", "0.00035"}, {200001, "flex", "0.0000875"},
	} {
		t.Run(fmt.Sprintf("%s/%d", test.tier, test.context), func(t *testing.T) {
			quote, err := billing.NewQuote(entries["m"].Price, "1", map[string]interface{}{"service_tier": test.tier}, time.Unix(1, 0))
			require.NoError(t, err)
			result, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: test.context, CacheReadTokens: 50, InputIncludesCache: true})
			require.NoError(t, err)
			assert.Equal(t, test.usd, result.TotalUSD)
		})
	}
}

func TestUnverifiedServiceTiersAreRejectedWithoutDefaultMultipliers(t *testing.T) {
	for _, source := range []struct {
		id   autopricing.SourceID
		data string
	}{
		{autopricing.SourceSub2API, `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`},
		{autopricing.SourceAxonHub, `{"p":[{"id":"m","cost":{"input":1,"output":2}}]}`},
	} {
		entries, err := autopricing.Parse(source.id, []byte(source.data))
		require.NoError(t, err)
		price := entries["m"].Price
		assert.Equal(t, []string{"default"}, price.Support.ServiceTiers)
		for _, tier := range []string{"priority", "fast", "ultrafast", "flex"} {
			err := billing.ValidatePricingRequirements(price, billing.PricingRequirements{ServiceTier: tier})
			require.Error(t, err)
			_, err = billing.NewQuote(price, "1", map[string]interface{}{"service_tier": tier}, time.Unix(1, 0))
			require.Error(t, err)
		}
		quote, err := billing.NewQuote(price, "1", nil, time.Unix(1, 0))
		require.NoError(t, err)
		charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000000})
		require.NoError(t, err)
		assert.Equal(t, "1", charge.TotalUSD)
	}
}

func TestExplicitServiceRequiresCompleteRatesAndPreservesZero(t *testing.T) {
	_, rejected, err := autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0}}`))
	require.Error(t, err)
	require.Len(t, rejected, 1)
	assert.Contains(t, rejected[0].Reason, "incomplete priority")
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0,"output_cost_per_token_priority":0}}`))
	require.NoError(t, err)
	quote, err := billing.NewQuote(entries["m"].Price, "1", map[string]interface{}{"service_tier": "priority"}, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000000, OutputTokens: 1000000})
	require.NoError(t, err)
	assert.Equal(t, "0", charge.TotalUSD)
}

func TestMultipleContextThresholdsAreRejectedWithoutInventedInheritance(t *testing.T) {
	for _, data := range []string{
		`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_above_100k_tokens":0.000002,"output_cost_per_token_above_200k_tokens":0.000004}}`,
		`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_above_100k_tokens":0.000002,"input_cost_per_token_above_200k_tokens_priority":0.000003}}`,
	} {
		_, rejected, err := autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(data))
		require.Error(t, err)
		require.Len(t, rejected, 1)
		assert.Contains(t, rejected[0].Reason, "multiple context thresholds")
	}
}

func TestAudioCachePriceIsExplicitAndDoesNotChargeAudioTwice(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{"audio":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"cache_read_input_token_cost":0.0000001,"input_cost_per_audio_token":0.000005,"cache_read_input_audio_token_cost":0}}`))
	require.NoError(t, err)
	price := entries["audio"].Price
	require.NoError(t, billing.ValidatePricingRequirements(price, billing.PricingRequirements{Dimensions: []string{"ai", "ai_cr"}}))
	quote, err := billing.NewQuote(price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	cached := int64(20)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, CacheReadTokens: 30, AudioInputTokens: 50, AudioCacheReadTokens: &cached, InputIncludesCache: true})
	require.NoError(t, err)
	assert.Equal(t, "0.000191", charge.TotalUSD)
}

func TestEmbeddingRequiresSourceTypeAndNeverCreatesAnOutputPrice(t *testing.T) {
	for _, test := range []struct {
		name   string
		source autopricing.SourceID
		data   string
	}{
		{"sub missing output", autopricing.SourceSub2API, `{"m":{"mode":"embedding","input_cost_per_token":0.000001}}`},
		{"sub explicit zero output", autopricing.SourceSub2API, `{"m":{"mode":"embedding","input_cost_per_token":0.000001,"output_cost_per_token":0}}`},
		{"axon missing output", autopricing.SourceAxonHub, `{"p":[{"id":"m","type":"embedding","cost":{"input":1}}]}`},
		{"axon explicit zero output", autopricing.SourceAxonHub, `{"p":[{"id":"m","type":"embedding","cost":{"input":1,"output":0}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, err := autopricing.Parse(test.source, []byte(test.data))
			require.NoError(t, err)
			price := entries["m"].Price
			assert.Equal(t, "embedding", price.UsageScope)
			assert.NotContains(t, price.Expression, "c *")
			assert.Empty(t, price.Prices)
			quote, err := billing.NewQuote(price, "1", map[string]interface{}{"service_tier": "default"}, time.Unix(1, 0))
			require.NoError(t, err)
			charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100})
			require.NoError(t, err)
			assert.Equal(t, "0.0001", charge.TotalUSD)
		})
	}
	for _, test := range []struct {
		name   string
		source autopricing.SourceID
		data   string
	}{
		{"sub missing type", autopricing.SourceSub2API, `{"m":{"input_cost_per_token":0.000001}}`},
		{"sub wrong type field", autopricing.SourceSub2API, `{"m":{"type":"embedding","input_cost_per_token":0.000001}}`},
		{"sub output charge", autopricing.SourceSub2API, `{"m":{"mode":"embedding","input_cost_per_token":0.000001,"output_cost_per_token":0.000001}}`},
		{"sub tier output charge", autopricing.SourceSub2API, `{"m":{"mode":"embedding","input_cost_per_token":0.000001,"output_cost_per_token_priority":0.000001}}`},
		{"axon missing type", autopricing.SourceAxonHub, `{"p":[{"id":"m","cost":{"input":1}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, rejected, err := autopricing.ParseWithReport(test.source, []byte(test.data))
			require.Error(t, err)
			assert.Empty(t, entries)
			assert.NotEmpty(t, rejected)
		})
	}
}

func TestProviderTierBoundaries(t *testing.T) {
	for _, test := range []struct {
		source          autopricing.SourceID
		data            string
		boundary, after string
	}{
		{autopricing.SourceAxonHub, `{"p":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":3,"output":4,"tier":{"type":"context","size":200000}}]}}}}}`, "0.0001", "0.0003"},
		{autopricing.SourceAxonHub, `{"p":[{"id":"m","cost":{"input":1,"output":2,"context_over_200k":{"input":3,"output":4}}}]}`, "0.0001", "0.0003"},
	} {
		t.Run(string(test.source), func(t *testing.T) {
			entries, err := autopricing.Parse(test.source, []byte(test.data))
			require.NoError(t, err)
			quote, err := billing.NewQuote(entries["m"].Price, "1", nil, time.Unix(1, 0))
			require.NoError(t, err)
			for index, length := range []int64{200000, 200001} {
				result, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: length})
				require.NoError(t, err)
				assert.Equal(t, []string{test.boundary, test.after}[index], result.TotalUSD)
			}
		})
	}
}

func TestRetiredAndUnknownSourcesAreRejected(t *testing.T) {
	for _, source := range []autopricing.SourceID{"models_dev", "openrouter", "unknown"} {
		entries, rejected, err := autopricing.ParseWithReport(source, []byte(`{"m":{"input_cost_per_token":0,"output_cost_per_token":0}}`))
		require.ErrorContains(t, err, "unsupported pricing source")
		assert.Empty(t, entries)
		assert.Empty(t, rejected)
	}
}

func TestOutputImagesArePerImageWithoutTokenDoubleCharge(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{"m":{"output_cost_per_image":0.04}}`))
	require.NoError(t, err)
	quote, err := billing.NewQuote(entries["m"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: 100, OutputTokens: 1100, ImageOutputTokens: 1000, Quantities: map[string]string{"images": "2"}})
	require.NoError(t, err)
	assert.Equal(t, "0.08", charge.TotalUSD)
	entries, err = autopricing.Parse(autopricing.SourceSub2API, []byte(`{"m":{"input_cost_per_token":0.000001,"output_cost_per_image":0.04}}`))
	require.NoError(t, err)
	quote, err = billing.NewQuote(entries["m"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err = billing.Calculate(quote, billing.Usage{InputTokens: 100, Quantities: map[string]string{"images": "2"}})
	require.NoError(t, err)
	assert.Equal(t, "0.0801", charge.TotalUSD)
	for _, source := range []string{
		`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"output_cost_per_image":0.04}}`,
		`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"output_cost_per_image_token":0.00004,"output_cost_per_image":0.04}}`,
	} {
		_, rejected, err := autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(source))
		require.Error(t, err)
		require.Len(t, rejected, 1)
		assert.True(t, rejected[0].BlockFallback)
		assert.Contains(t, rejected[0].Reason, "missing usage contract")
	}
}

func TestSubServicePriceThenContextRatioAndExplicitOverrides(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{"m":{
	 "input_cost_per_token":0.0000025,"output_cost_per_token":0.000015,"cache_read_input_token_cost":0.00000025,
	 "input_cost_per_token_above_272k_tokens":0.000005,"output_cost_per_token_above_272k_tokens":0.0000225,
	 "cache_read_input_token_cost_above_272k_tokens":0.0000005,
	 "input_cost_per_token_priority":0.000005,"output_cost_per_token_priority":0.00003,"cache_read_input_token_cost_priority":0.0000005,
	 "input_cost_per_token_flex":0.00000125,"output_cost_per_token_flex":0.0000075,"cache_read_input_token_cost_flex":0.000000125,
	 "input_cost_per_token_above_272k_tokens_flex":0.0000025,"output_cost_per_token_above_272k_tokens_flex":0.00001125,
	 "cache_read_input_token_cost_above_272k_tokens_flex":0.00000025
	}}`))
	require.NoError(t, err)
	for _, test := range []struct {
		tier    string
		context int64
		usd     string
	}{
		{"priority", 272000, "0.000575"}, {"priority", 272001, "0.001"}, {"flex", 272001, "0.00025"},
	} {
		quote, err := billing.NewQuote(entries["m"].Price, "1", map[string]interface{}{"service_tier": test.tier}, time.Unix(1, 0))
		require.NoError(t, err)
		charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, OutputTokens: 10, ContextTokens: test.context, CacheReadTokens: 50, InputIncludesCache: true})
		require.NoError(t, err)
		assert.Equal(t, test.usd, charge.TotalUSD)
	}
}

func TestLegacyTierMirrorUsesTypedThresholdAndRejectsContradiction(t *testing.T) {
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(`{"p":{"models":{
	 "m":{"cost":{"input":1,"output":2,"context_over_200k":{"input":3,"output":4},"tiers":[{"input":3,"output":4,"tier":{"type":"context","size":272000}}]}},
	 "bad":{"cost":{"input":1,"output":2,"context_over_200k":{"input":30,"output":40},"tiers":[{"input":3,"output":4,"tier":{"type":"context","size":272000}}]}}
	}}}`))
	require.NoError(t, err)
	require.Contains(t, entries, "m")
	assert.NotContains(t, entries, "bad")
	require.Len(t, rejected, 2)
	for _, r := range rejected {
		assert.True(t, r.BlockFallback)
		assert.Equal(t, "conflicting context tier formats", r.Reason)
	}
	quote, err := billing.NewQuote(entries["m"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	for index, length := range []int64{200001, 272000, 272001} {
		charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: length})
		require.NoError(t, err)
		assert.Equal(t, []string{"0.0001", "0.0001", "0.0003"}[index], charge.TotalUSD)
	}
}

func TestRangeTiersAndExplicitMultiplierCache(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{
	 "range":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"long_context_threshold_inclusive":true,"tiered_pricing":[{"range":[0,32000],"input_cost_per_token":0.000001,"output_cost_per_token":0.000002},{"range":[32000,200000],"input_cost_per_token":0.000003,"output_cost_per_token":0.000004}]},
	 "multiplier":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"cache_read_input_token_cost":0.0000001,"cache_creation_input_token_cost":0.00000125,"cache_creation_input_token_cost_above_1hr":0.000002,"long_context_input_token_threshold":32000,"long_context_input_cost_multiplier":2,"long_context_output_cost_multiplier":3},
	 "disabled":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"long_context_input_token_threshold":0,"long_context_input_cost_multiplier":2}
	}`))
	require.NoError(t, err)
	require.Len(t, entries, 3)
	quote, err := billing.NewQuote(entries["range"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: 32000})
	require.NoError(t, err)
	assert.Equal(t, "0.0003", charge.TotalUSD)
	quote, err = billing.NewQuote(entries["multiplier"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err = billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: 32001, CacheReadTokens: 30, CacheWriteTokens: 20, CacheWrite1hTokens: 10, InputIncludesCache: true})
	require.NoError(t, err)
	assert.Equal(t, "0.000176", charge.TotalUSD)
	assert.Equal(t, billing.ModeExpression, entries["disabled"].Price.Mode)
	quote, err = billing.NewQuote(entries["disabled"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err = billing.Calculate(quote, billing.Usage{InputTokens: 100, ContextTokens: 1000000})
	require.NoError(t, err)
	assert.Equal(t, "0.0001", charge.TotalUSD)
}

func TestUnsupportedAndInvalidTiersAreRejectedWithinSource(t *testing.T) {
	for _, data := range []string{
		`{"p":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":3,"tier":{"type":"context","size":100}},{"input":4,"tier":{"type":"context","size":100}}]}}}}}`,
		`{"p":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":3,"tier":{"type":"duration","size":100}}]}}}}}`,
		`{"p":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":-1,"tier":{"type":"context","size":100}}]}}}}}`,
	} {
		_, rejected, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(data))
		require.Error(t, err)
		require.NotEmpty(t, rejected)
		for _, r := range rejected {
			assert.True(t, r.BlockFallback)
		}
	}
}

func TestMediaCacheAndSearchPrices(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{
	 "media":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"cache_read_input_token_cost":0,"cache_creation_input_token_cost":0.000003,"cache_creation_input_token_cost_above_1hr":0.000004,"input_cost_per_image_token":0.000005,"output_cost_per_image_token":0.000006,"cache_read_input_image_token_cost":0.0000005,"input_cost_per_audio_token":0.000007,"output_cost_per_audio_token":0.000008},
	 "image":{"output_cost_per_image":0.04},
	 "image-token":{"mode":"image_generation","input_cost_per_token":0.000005,"output_cost_per_image_token":0.00004},
	 "search":{"input_cost_per_token":0,"output_cost_per_token":0,"litellm_provider":"anthropic","search_context_cost_per_query":{"search_context_size_low":0.01,"search_context_size_medium":0.01,"search_context_size_high":0.01}}
	}`))
	require.NoError(t, err)
	require.Len(t, entries, 4)
	assert.Empty(t, entries["media"].Price.Prices)
	mediaQuote, err := billing.NewQuote(entries["media"].Price, "1", map[string]interface{}{"service_tier": "default"}, time.Unix(1, 0))
	require.NoError(t, err)
	mediaCharge, err := billing.Calculate(mediaQuote, billing.Usage{InputTokens: 20, OutputTokens: 10, ImageInputTokens: 5, ImageOutputTokens: 3, AudioInputTokens: 5, AudioOutputTokens: 2})
	require.NoError(t, err)
	assert.Equal(t, "0.000114", mediaCharge.TotalUSD)
	quote, err := billing.NewQuote(entries["image"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{Quantities: map[string]string{"images": "3"}})
	require.NoError(t, err)
	assert.Equal(t, "0.12", charge.TotalUSD)
	quote, err = billing.NewQuote(entries["search"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err = billing.Calculate(quote, billing.Usage{Quantities: map[string]string{"tool:web_search": "2"}})
	require.NoError(t, err)
	assert.Equal(t, "0.02", charge.TotalUSD)
}

func TestUnenabledOptionalToolsDoNotDisableOrdinaryPricing(t *testing.T) {
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(`{
	 "maps":{"litellm_provider":"gemini","input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"google_maps_grounding_cost_per_query":0.01},
	 "search":{"litellm_provider":"openai","input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"search_context_cost_per_query":{"search_context_size_low":0.01,"search_context_size_medium":0.02,"search_context_size_high":0.03}}
	}`))
	require.NoError(t, err)
	require.Empty(t, rejected)
	for _, name := range []string{"maps", "search"} {
		entry := entries[name]
		require.NoError(t, billing.ValidatePricingRequirements(entry.Price, billing.PricingRequirements{Dimensions: []string{"p", "c"}}))
		quote, err := billing.NewQuote(entry.Price, "1", nil, time.Unix(1, 0))
		require.NoError(t, err)
		charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 3, OutputTokens: 1})
		require.NoError(t, err)
		assert.Equal(t, "0.000005", charge.TotalUSD)
	}
	for _, test := range []struct{ model, dimension string }{{"maps", "tool:google_maps"}, {"search", "tool:web_search"}, {"search", "tool:web_search_preview"}} {
		err := billing.ValidatePricingRequirements(entries[test.model].Price, billing.PricingRequirements{Dimensions: []string{test.dimension}})
		var missing *billing.PricingError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "missing_price_dimension", missing.Code)
		assert.Equal(t, test.dimension, missing.Dimension)
	}
}

func TestProviderAmbiguityDoesNotOverrideCanonicalPrices(t *testing.T) {
	entries, rejections, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(`{
	 "a":{"models":{"m":{"cost":{"input":1,"output":2}},"same":{"cost":{"input":1,"output":2}}}},
	 "b":{"models":{"m":{"cost":{"input":3,"output":4}},"same":{"cost":{"input":1,"output":2}},"a/m":{"cost":{"input":9,"output":10}}}}
	}`))
	require.NoError(t, err)
	assert.NotContains(t, entries, "m")
	assert.NotContains(t, entries, "same")
	assert.Contains(t, entries, "a/m")
	assert.Contains(t, entries, "b/m")
	quote, err := billing.NewQuote(entries["a/m"].Price, "1", nil, time.Unix(1, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100})
	require.NoError(t, err)
	assert.Equal(t, "0.0001", charge.TotalUSD)
	require.Len(t, rejections, 2)
	assert.Equal(t, "m", rejections[0].Model)
	assert.Equal(t, "same", rejections[1].Model)
	for _, rejection := range rejections {
		assert.True(t, rejection.BlockFallback)
		assert.Equal(t, "ambiguous provider identity", rejection.Reason)
	}
}

func TestUnpricedProviderStillMakesBareIdentityAmbiguous(t *testing.T) {
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(`{
	 "a":[{"id":"m","cost":{"input":1,"output":2}}],
	 "b":[{"id":"m"}]
	}`))
	require.NoError(t, err)
	assert.Contains(t, entries, "a/m")
	assert.NotContains(t, entries, "b/m")
	assert.NotContains(t, entries, "m")
	assert.Contains(t, rejected, autopricing.Rejection{Model: "m", Reason: "ambiguous provider identity", BlockFallback: true})
	assert.Contains(t, rejected, autopricing.Rejection{Model: "b/m", Provider: "b", Reason: "missing cost", BlockFallback: false})
}

func TestConflictingDuplicateIdentityCannotDependOnArrayOrder(t *testing.T) {
	for _, records := range []string{
		`{"id":"m","cost":{"input":1,"output":2}},{"id":"m","cost":{"input":2,"output":2}}`,
		`{"id":"m","cost":{"input":1,"output":2}},{"id":"m"}`,
		`{"id":"m"},{"id":"m","cost":{"input":1,"output":2}}`,
	} {
		entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(`{"p":[{"id":"valid","cost":{"input":0,"output":0}},`+records+`]}`))
		require.NoError(t, err)
		assert.Contains(t, entries, "valid")
		assert.NotContains(t, entries, "p/m")
		assert.NotContains(t, entries, "m")
		require.Len(t, rejected, 2)
		assert.Equal(t, "m", rejected[0].Model)
		assert.Equal(t, "p/m", rejected[1].Model)
	}
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(`{"p":[{"id":"m","cost":{"input":1,"output":2}},{"id":"m","cost":{"input":1,"output":2}}]}`))
	require.NoError(t, err)
	assert.Contains(t, entries, "m")
	assert.Empty(t, rejected)
}

func TestDamagedCatalogCannotBecomeAPartialSnapshot(t *testing.T) {
	for _, data := range []string{
		`{"providers":[]}`,
		`{"p":[{"id":"valid","cost":{"input":0,"output":0}},42]}`,
		`{"p":[{"id":"valid","cost":{"input":0,"output":0}},{"cost":{"input":0,"output":0}}]}`,
		`{"p":[{"id":"valid","cost":{"input":0,"output":0}}],"broken":{"models":null}}`,
	} {
		entries, _, err := autopricing.ParseWithReport(autopricing.SourceAxonHub, []byte(data))
		require.Error(t, err)
		assert.Empty(t, entries)
	}
	for _, source := range []autopricing.SourceID{autopricing.SourceSub2API, autopricing.SourceAxonHub} {
		entries, _, err := autopricing.ParseWithReport(source, []byte(`{}`))
		require.ErrorContains(t, err, "no usable prices")
		assert.Empty(t, entries)
	}
}

func TestDecimalPrecisionCannotBeRoundedIntoAValidPrice(t *testing.T) {
	entries, rejected, err := autopricing.ParseWithReport(autopricing.SourceSub2API, []byte(`{
	 "exact":{"input_cost_per_token":1e-24,"output_cost_per_token":0},
	 "too-precise":{"input_cost_per_token":1e-25,"output_cost_per_token":0},
	 "too-large":{"input_cost_per_token":1e19,"output_cost_per_token":0}
	}`))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Contains(t, entries["exact"].Price.Expression, "p * 0.000000000000000001")
	require.Len(t, rejected, 2)
	for _, rejection := range rejected {
		assert.True(t, rejection.BlockFallback)
	}
}

func TestResolvedAliasesKeepThePublicationHashStable(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceAxonHub, []byte(`{"p":[{"id":"m","cost":{"input":1,"output":2}}]}`))
	require.NoError(t, err)
	for _, model := range []string{"m", "p/m", "models/m", "m-20260926"} {
		entry, ok := autopricing.Resolve(entries, model)
		require.True(t, ok, model)
		entry.Price.Model = model
		published, err := billing.PreparePriceVersion(entry.Price)
		require.NoError(t, err)
		draftHash, err := billing.Hash(entry.Price)
		require.NoError(t, err)
		publishedHash, err := billing.Hash(published)
		require.NoError(t, err)
		assert.Equal(t, publishedHash, draftHash, model)
	}
}

func TestResolveExactSafeAliasesAndAmbiguity(t *testing.T) {
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(`{"gemini-pro":{"input_cost_per_token":0,"output_cost_per_token":0},"chat-2026-09-20":{"input_cost_per_token":0,"output_cost_per_token":0},"chat-20260921":{"input_cost_per_token":1,"output_cost_per_token":2},"single-20260920":{"input_cost_per_token":0,"output_cost_per_token":0}}`))
	require.NoError(t, err)
	for _, model := range []string{"gemini-pro", "models/gemini-pro", "publishers/google/models/gemini-pro", "projects/p/locations/us/publishers/google/models/gemini-pro", "gemini-pro-20260926", "models/gemini-pro-2026-09-26"} {
		_, ok := autopricing.Resolve(entries, model)
		assert.True(t, ok, model)
	}
	for _, model := range []string{"chat", "chat-20260922", "single", "single-20260921", "gemini-flash", "gemini-pro-20260230", "untrusted/gemini-pro"} {
		_, ok := autopricing.Resolve(entries, model)
		assert.False(t, ok, model)
	}
	entry, ok := autopricing.Resolve(entries, "chat-20260921")
	require.True(t, ok)
	assert.Equal(t, "chat-20260921", entry.SourceModel)
}

func TestIsBlockedFollowsOneWayAliases(t *testing.T) {
	for _, test := range []struct {
		rejected          string
		blocked, accepted []string
	}{
		{"unsafe", []string{"unsafe", "models/unsafe", "unsafe-20260920", "publishers/google/models/unsafe-2026-09-23"}, []string{"safe", "provider/unsafe", "unsafe-20260230"}},
		{"models/unsafe-20260920", []string{"unsafe-20260920", "projects/p/locations/us/publishers/google/models/unsafe-20260920"}, []string{"unsafe", "unsafe-20260921", "models/unsafe-2026-09-20"}},
	} {
		t.Run(test.rejected, func(t *testing.T) {
			rejected := map[string]bool{test.rejected: true, "available": false}
			for _, model := range test.blocked {
				assert.True(t, autopricing.IsBlocked(rejected, model), model)
			}
			for _, model := range append(test.accepted, "available", "models/available") {
				assert.False(t, autopricing.IsBlocked(rejected, model), model)
			}
		})
	}
	assert.False(t, autopricing.IsBlocked(nil, "unsafe"))
}

// This optional offline integration check validates caller-supplied source
// snapshots without making tests depend on network availability or local paths.
func TestLocalCatalogSnapshots(t *testing.T) {
	directory := os.Getenv("AUTOPRICING_SNAPSHOT_DIR")
	if directory == "" {
		t.Skip("no offline source snapshots supplied")
	}
	for _, source := range []autopricing.SourceID{autopricing.SourceSub2API, autopricing.SourceAxonHub} {
		t.Run(string(source), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(directory, "ren2hub-pricing-"+string(source)+".json"))
			require.NoError(t, err)
			entries, rejected, err := autopricing.ParseWithReport(source, data)
			require.NoError(t, err)
			require.NotEmpty(t, entries)
			for _, entry := range entries {
				require.NoError(t, billing.ValidatePriceVersion(entry.Price), entry.Price.Model)
			}
			counts := map[string]int{}
			for _, item := range rejected {
				counts[item.Reason]++
			}
			reasons := make([]string, 0, len(counts))
			for reason := range counts {
				reasons = append(reasons, reason)
			}
			sort.Slice(reasons, func(i, j int) bool { return counts[reasons[i]] > counts[reasons[j]] })
			t.Logf("accepted=%d rejected=%d", len(entries), len(rejected))
			for i, reason := range reasons {
				if i >= 8 {
					break
				}
				t.Logf("%d %s", counts[reason], reason)
			}
			for _, model := range []string{"gpt-5.4", "gpt-5.4-pro", "claude-sonnet-4-6", "gpt-image-1", "glm-5"} {
				if entry, ok := entries[model]; ok {
					t.Logf("sample %s accepted %s", model, entry.Price.Mode)
				} else {
					for _, r := range rejected {
						if r.Model == model {
							t.Logf("sample %s rejected %s", model, r.Reason)
							break
						}
					}
				}
			}
		})
	}
}
