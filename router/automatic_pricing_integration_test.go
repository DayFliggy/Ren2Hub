package router

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func publishAutomaticGatewayFixture(t *testing.T, name, document string) {
	t.Helper()
	entries, err := autopricing.Parse(autopricing.SourceSub2API, []byte(document))
	require.NoError(t, err)
	entry, exists := entries[name]
	require.True(t, exists)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(document)))
	require.NoError(t, model.DB.AutoMigrate(&model.BillingPricingSource{}, &model.BillingPricingSnapshot{}))
	require.NoError(t, model.EnsureBillingPricingSource(context.Background(), &model.BillingPricingSource{ParserVersion: autopricing.ParserVersion, Source: string(entry.Source), Hash: hash, Document: model.LongText(document)}))
	published, err := model.PublishAutomaticBillingPrice(context.Background(), entry.Price, string(entry.Source), entry.SourceModel, hash)
	require.NoError(t, err)
	require.True(t, published)
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	channel.Models = name
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"models": name, "setting": `{"pass_through_body_enabled":true}`}).Error)
	require.NoError(t, channel.UpdateAbilities(model.DB))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 1000000}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
}

func TestGatewayAutomaticEmbeddingPricing(t *testing.T) {
	for _, test := range []struct {
		name, usage string
		hold        bool
	}{
		{"input_only", `{"prompt_tokens":1000,"total_tokens":1000}`, false},
		{"completion_tokens", `{"prompt_tokens":1000,"completion_tokens":1,"total_tokens":1001}`, true},
		{"output_tokens_alias", `{"prompt_tokens":1000,"output_tokens":1,"total_tokens":1001}`, true},
		{"reasoning_detail", `{"prompt_tokens":1000,"total_tokens":1000,"completion_tokens_details":{"reasoning_tokens":1}}`, true},
		{"text_detail", `{"prompt_tokens":1000,"total_tokens":1000,"completion_tokens_details":{"text_tokens":1}}`, true},
		{"audio_detail", `{"prompt_tokens":1000,"total_tokens":1000,"completion_tokens_details":{"audio_tokens":1}}`, true},
		{"image_detail", `{"prompt_tokens":1000,"total_tokens":1000,"completion_tokens_details":{"image_tokens":1}}`, true},
		{"output_details_alias", `{"prompt_tokens":1000,"total_tokens":1000,"output_tokens_details":{"audio_tokens":1}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/v1/embeddings", r.URL.Path)
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.Positive(t, user.ReservedQuota, "reserve the scoped input price before sending")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"object":"list","data":[{"index":0,"embedding":[0,1]}],"usage":%s}`, test.usage)
			})
			publishAutomaticGatewayFixture(t, "text-embedding-3-small", `{"text-embedding-3-small":{"mode":"embedding","input_cost_per_token":0.000001}}`)
			status, body := gatewayLegacyOperationRequest(t, base, key, "/v1/embeddings", `{"model":"text-embedding-3-small","input":"hello"}`)
			assert.EqualValues(t, 1, sends.Load())
			if !test.hold {
				require.Equal(t, http.StatusOK, status, body)
			}
			awaitGatewayPermitsReleased(t)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			var quote billing.Quote
			require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
			assert.Equal(t, billing.UsageScopeEmbedding, quote.PriceVersion.UsageScope)
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			if test.hold {
				assert.Equal(t, model.BillingRecoveryStatusReconciliation, recovery.Status)
				assert.True(t, model.BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState))
				assert.Empty(t, recovery.ChargeSnapshot)
				assert.Positive(t, recovery.FundingAmount)
				assert.Equal(t, recovery.FundingAmount, user.ReservedQuota)
				assert.Equal(t, recovery.TokenAmount, token.ReservedQuota)
				assert.Zero(t, user.UsedQuota)
				assert.Zero(t, token.UsedQuota)
				return
			}
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			assert.Equal(t, model.BillingRecoveryStateCompleted, recovery.ConsumptionState)
			assert.EqualValues(t, 500, user.UsedQuota)
			assert.EqualValues(t, 500, token.UsedQuota)
			assert.Zero(t, user.ReservedQuota)
			assert.Zero(t, token.ReservedQuota)
			var usage billing.Usage
			require.NoError(t, common.UnmarshalJsonStr(recovery.UsageSnapshot, &usage))
			assert.NotContains(t, usage.Quantities, "calls", "token-only embeddings cannot inherit a generic per-call quantity")
			charge, err := billing.Calculate(&quote, usage)
			require.NoError(t, err)
			assert.Equal(t, "0.001", charge.TotalUSD)
		})
	}
}

func TestGatewayAutomaticEmbeddingPriceRejectsChatBeforeSend(t *testing.T) {
	var sends atomic.Int32
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1) })
	publishAutomaticGatewayFixture(t, "text-embedding-3-small", `{"text-embedding-3-small":{"mode":"embedding","input_cost_per_token":0.000001}}`)
	status, _ := gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", `{"model":"text-embedding-3-small","messages":[{"role":"user","content":"hello"}]}`)
	assert.NotEqual(t, http.StatusOK, status)
	assert.Zero(t, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
	assert.Zero(t, count, "a scope mismatch must not reserve funds")
}

func TestGatewayAutomaticServiceTierFrozenSettlement(t *testing.T) {
	for _, test := range []struct {
		tier  string
		quota common.Quota
	}{
		{"priority", 1200},
		{"flex", 300},
		{"default", 600},
	} {
		t.Run(test.tier, func(t *testing.T) {
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					assert.Equal(t, test.tier, request["service_tier"], "billing cannot rewrite the upstream body")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"automatic-tier","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":100,"total_tokens":1100}}`)
			})
			publishAutomaticGatewayFixture(t, "automatic-tier-model", `{"automatic-tier-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0.000002,"output_cost_per_token_priority":0.000004,"input_cost_per_token_flex":0.0000005,"output_cost_per_token_flex":0.000001}}`)
			payload, err := common.Marshal(map[string]any{"model": "automatic-tier-model", "messages": []map[string]string{{"role": "user", "content": "hello"}}, "service_tier": test.tier})
			require.NoError(t, err)
			request, err := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", strings.NewReader(string(payload)))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode, string(body))
			awaitGatewayPermitsReleased(t)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			require.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			var quote billing.Quote
			var usage billing.Usage
			var charge billing.ChargeResult
			require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
			require.NoError(t, common.UnmarshalJsonStr(recovery.UsageSnapshot, &usage))
			require.NoError(t, common.UnmarshalJsonStr(recovery.ChargeSnapshot, &charge))
			assert.Equal(t, test.tier, quote.Attributes["service_tier"])
			assert.Equal(t, test.quota, charge.FinalQuota)
			_, err = model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "automatic-tier-model", Mode: billing.ModeExpression, Expression: "p * 100"}, quote.PriceVersion.ID, 1)
			require.NoError(t, err)
			replayed, err := billing.Calculate(&quote, usage)
			require.NoError(t, err)
			assert.Equal(t, charge, *replayed, "settlement keeps the published service rules frozen after later manual edits")
		})
	}
}

func TestGatewayManualServiceTierKeepsCaseSensitiveExpression(t *testing.T) {
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
			assert.Equal(t, "FAST", request["service_tier"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"manual-tier","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":0,"total_tokens":1000}}`)
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Update("setting", `{"pass_through_body_enabled":true}`).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	current, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
	require.NoError(t, err)
	_, err = model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "gpt-4o-mini", Mode: billing.ModeExpression, Support: &billing.PricingSupport{Dimensions: []string{"p", "c"}, ServiceTiers: []string{"default", "FAST"}}, Expression: `p * (attr("service_tier") == "FAST" ? 7 : 1) + c * 0`}, current.ID, 1)
	require.NoError(t, err)
	status, body := gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"service_tier":"FAST"}`)
	require.Equal(t, http.StatusOK, status, body)
	awaitGatewayPermitsReleased(t)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery).Error)
	require.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
	var quote billing.Quote
	var charge billing.ChargeResult
	require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
	require.NoError(t, common.UnmarshalJsonStr(recovery.ChargeSnapshot, &charge))
	assert.Equal(t, "FAST", quote.Attributes["service_tier"])
	assert.EqualValues(t, 3500, charge.FinalQuota)
}

func TestGatewayAutomaticUnavailablePriceStopsAliasFallback(t *testing.T) {
	var sends atomic.Int32
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"unavailable-restore","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":0,"total_tokens":1000}}`)
	})
	publishAutomaticGatewayFixture(t, "gpt-5.4", `{"gpt-5.4":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	channel.Models = "gpt-5.4,gpt-5.4-high,gpt-5.4@effort:high"
	require.NoError(t, model.DB.Model(channel).Update("models", channel.Models).Error)
	require.NoError(t, channel.UpdateAbilities(model.DB))
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high@thinking:on"} {
		require.NoError(t, model.DB.Create(&model.BillingActivePrice{Model: name, Origin: "unavailable"}).Error)
	}
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high"} {
		payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, name)
		status, body := gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", payload)
		assert.NotEqual(t, http.StatusOK, status, body)
		assert.Zero(t, sends.Load(), "an unavailable specific price cannot fall through to its valid base price")
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
	assert.Zero(t, count)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Zero(t, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high@thinking:on"} {
		_, err = model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: name, Mode: billing.ModeExpression, Expression: "p * 3 + c * 0"}, 0, 1)
		require.NoError(t, err)
	}
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high"} {
		payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, name)
		status, body := gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", payload)
		require.Equal(t, http.StatusOK, status, body)
		awaitGatewayPermitsReleased(t)
	}
	assert.EqualValues(t, 2, sends.Load())
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 3000, user.UsedQuota)
	assert.Zero(t, user.ReservedQuota)
}

func TestGatewayDispatchedQuoteSettlesAfterAutomaticPriceStops(t *testing.T) {
	const name = "gpt-5.4-high"
	var sends atomic.Int32
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var user model.User
		if assert.NoError(t, model.DB.First(&user, 1).Error) {
			assert.Positive(t, user.ReservedQuota)
		}
		// A refresh after dispatch stops future requests, but cannot replace
		// the immutable quote already attached to this reservation.
		assert.NoError(t, model.DB.Model(&model.BillingActivePrice{}).Where("model = ?", name).Updates(map[string]any{"version_id": 0, "origin": "unavailable"}).Error)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"already-dispatched","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":0,"total_tokens":1000}}`)
	})
	publishAutomaticGatewayFixture(t, name, `{"gpt-5.4-high":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000006}}`)
	body := `{"model":"gpt-5.4-high","messages":[{"role":"user","content":"hello"}]}`
	status, result := gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", body)
	require.Equal(t, http.StatusOK, status, result)
	awaitGatewayPermitsReleased(t)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
	var quote billing.Quote
	var usage billing.Usage
	require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
	require.NoError(t, common.UnmarshalJsonStr(recovery.UsageSnapshot, &usage))
	charge, err := billing.Calculate(&quote, usage)
	require.NoError(t, err)
	assert.EqualValues(t, 1500, charge.FinalQuota)
	status, result = gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", body)
	assert.NotEqual(t, http.StatusOK, status, result)
	assert.EqualValues(t, 1, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestGatewayAutomaticUnverifiedServiceTierStopsBeforeReserve(t *testing.T) {
	for _, tier := range []string{"priority", "fast", "ultrafast", "flex", "unknown"} {
		t.Run(tier, func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1) })
			publishAutomaticGatewayFixture(t, "unsupported-tier", `{"unsupported-tier":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`)
			payload := fmt.Sprintf(`{"model":"unsupported-tier","messages":[{"role":"user","content":"hello"}],"service_tier":%q}`, tier)
			status, body := gatewayLegacyOperationRequest(t, base, key, "/v1/chat/completions", payload)
			assert.Equal(t, http.StatusServiceUnavailable, status, body)
			assert.Zero(t, sends.Load())
			var count int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}
