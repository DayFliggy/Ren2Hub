package billing_test

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEncryptedEvidenceReplaysDynamicInputsAfterRestart(t *testing.T) {
	version, err := billing.NewModelPriceVersion("dynamic", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p * param(param("key")) * (header(param("header")) == "secret-token" ? 2 : 1) * (hour("UTC") < 12 ? 1 : 3)`}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1.25", nil, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	key := []byte("unit-test-persistent-secret-not-production")
	cipher, err := billing.NewEvidenceCipher(key)
	require.NoError(t, err)
	binding := billing.EvidenceBinding{RequestID: "r1", AttemptID: "a1", ConfigHash: billing.PricingConfigHash(quote), Phase: "settlement"}
	clock := billingexpr.NewEvaluation()
	clock.Now = func() time.Time { return time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC) }
	input := billingexpr.RequestInput{Body: []byte(`{"key":"prices.actual","prices":{"actual":4},"header":"Authorization","prompt":"private prompt"}`), Headers: map[string]string{"Authorization": "secret-token"}, Evaluation: clock}
	usage, charge, err := cipher.Evaluate(quote, billing.Usage{InputTokens: 100, ContextTokens: 100, InputIncludesCache: true, Complete: true}, input, binding)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(1500), charge.FinalQuota)
	encoded, err := common.Marshal(usage)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "secret-token")
	assert.NotContains(t, string(encoded), "private prompt")
	assert.NotContains(t, string(encoded), "Authorization")
	var stored billing.Usage
	require.NoError(t, common.Unmarshal(encoded, &stored))
	restarted, err := billing.NewEvidenceCipher(key)
	require.NoError(t, err)
	assert.Equal(t, cipher.KeyID(), restarted.KeyID())
	billingexpr.InvalidateCache()
	replayed, err := restarted.Replay(quote, stored, binding)
	require.NoError(t, err)
	assert.Equal(t, charge, replayed)
	_, err = billing.Calculate(quote, stored)
	require.Error(t, err)
	for _, tc := range []struct {
		name   string
		mutate func(*billing.Usage, *billing.EvidenceBinding)
	}{
		{"different request", func(_ *billing.Usage, b *billing.EvidenceBinding) { b.RequestID = "r2" }},
		{"different phase", func(_ *billing.Usage, b *billing.EvidenceBinding) { b.Phase = "estimate" }},
		{"changed usage", func(u *billing.Usage, _ *billing.EvidenceBinding) { u.InputTokens++ }},
		{"ciphertext tampering", func(u *billing.Usage, _ *billing.EvidenceBinding) { u.Evidence.Ciphertext[0] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var changed billing.Usage
			require.NoError(t, common.Unmarshal(encoded, &changed))
			otherBinding := binding
			tc.mutate(&changed, &otherBinding)
			_, err := restarted.Replay(quote, changed, otherBinding)
			require.ErrorIs(t, err, billing.ErrEvaluationEvidence)
		})
	}
	wrong, err := billing.NewEvidenceCipher([]byte("another-secret"))
	require.NoError(t, err)
	_, err = wrong.Replay(quote, stored, binding)
	require.ErrorIs(t, err, billing.ErrEvaluationEvidence)
}

func TestEstimateAndSettlementKeepIndependentBranchesAndClocks(t *testing.T) {
	version, err := billing.NewModelPriceVersion("branch", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `len < 10 ? p * param("estimate") : p * param("actual") * (hour("UTC") < 12 ? 1 : 2)`}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	cipher, err := billing.NewEvidenceCipher([]byte("persistent-test-key"))
	require.NoError(t, err)
	for _, tc := range []struct {
		phase string
		count int64
		hour  int
		want  common.Quota
	}{{"estimate", 4, 11, 4}, {"settlement", 20, 13, 200}} {
		t.Run(tc.phase, func(t *testing.T) {
			clock := billingexpr.NewEvaluation()
			clock.Now = func() time.Time { return time.Date(2026, 9, 30, tc.hour, 0, 0, 0, time.UTC) }
			binding := billing.EvidenceBinding{RequestID: "r", AttemptID: "a", ConfigHash: billing.PricingConfigHash(quote), Phase: tc.phase}
			usage, charge, err := cipher.Evaluate(quote, billing.Usage{InputTokens: tc.count, ContextTokens: tc.count, Complete: true, Estimated: tc.phase == "estimate"}, billingexpr.RequestInput{Body: []byte(`{"estimate":2,"actual":10}`), Evaluation: clock}, binding)
			require.NoError(t, err)
			assert.Equal(t, tc.want, charge.FinalQuota)
			replayed, err := cipher.Replay(quote, usage, binding)
			require.NoError(t, err)
			assert.Equal(t, charge, replayed)
		})
	}
}

func TestNewAPIModelModesUseTheirOwnUnitsAndRounding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values billing.PricingValues
		usage  billing.Usage
		task   bool
		input  billingexpr.RequestInput
		want   common.Quota
	}{
		{"ratio", billing.PricingValues{"ModelRatio": 2.0, "CompletionRatio": 3.0, "CacheRatio": 0.1}, billing.Usage{InputTokens: 100, ContextTokens: 100, OutputTokens: 10, CacheReadTokens: 20, InputIncludesCache: true, Complete: true}, false, billingexpr.RequestInput{}, 224},
		{"per image", billing.PricingValues{"ModelPrice": 0.04}, billing.Usage{Complete: true, Quantities: map[string]string{"images": "3"}}, false, billingexpr.RequestInput{}, 60000},
		{"explicit zero", billing.PricingValues{"ModelRatio": 0.0}, billing.Usage{InputTokens: 100, ContextTokens: 100, Complete: true}, false, billingexpr.RequestInput{}, 0},
		{"expression rounds", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p*0.7+c*0.9"}, billing.Usage{InputTokens: 3, ContextTokens: 3, OutputTokens: 1, Complete: true}, false, billingexpr.RequestInput{}, 1},
		{"task USD", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("seconds")*0.4`}, billing.Usage{Complete: true}, true, billingexpr.RequestInput{Usage: map[string]any{"seconds": float64(10)}}, 2000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version, err := billing.NewModelPriceVersion(tc.name, tc.values, tc.task)
			require.NoError(t, err)
			quote, err := billing.NewQuote(version, "1", nil, time.Now())
			require.NoError(t, err)
			cipher, err := billing.NewEvidenceCipher([]byte("test-key"))
			require.NoError(t, err)
			binding := billing.EvidenceBinding{RequestID: "r", AttemptID: "a", ConfigHash: billing.PricingConfigHash(quote), Phase: "settlement"}
			usage, charge, err := cipher.Evaluate(quote, tc.usage, tc.input, binding)
			require.NoError(t, err)
			assert.Equal(t, tc.want, charge.FinalQuota)
			replayed, err := cipher.Replay(quote, usage, binding)
			require.NoError(t, err)
			assert.Equal(t, charge, replayed)
		})
	}
	_, err := billing.NewModelPriceVersion("missing", billing.PricingValues{}, false)
	require.Error(t, err)
}

func TestDynamicTierNamesNeverBecomePlaintextFinancialEvidence(t *testing.T) {
	const expression = `tier(header("Authorization"), p*2)`
	version, err := billing.NewModelPriceVersion("private-tier", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	cipher, err := billing.NewEvidenceCipher([]byte("test-key"))
	require.NoError(t, err)
	input := billingexpr.RequestInput{Headers: map[string]string{"Authorization": "private-test-credential"}}
	_, rawTrace, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: 10}, input)
	require.NoError(t, err)
	assert.Equal(t, "private-test-credential", rawTrace.MatchedTier, "the VM retains upstream expression behavior")
	binding := billing.EvidenceBinding{RequestID: "r", AttemptID: "a", ConfigHash: billing.PricingConfigHash(quote), Phase: "settlement"}
	usage, charge, err := cipher.Evaluate(quote, billing.Usage{InputTokens: 10, InputIncludesCache: true}, input, binding)
	require.NoError(t, err)
	assert.EqualValues(t, 10, charge.FinalQuota)
	assert.Empty(t, charge.Trace.MatchedTier)
	for _, value := range []any{usage, charge} {
		encoded, err := common.Marshal(value)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "private-test-credential")
	}
	replayed, err := cipher.Replay(quote, usage, binding)
	require.NoError(t, err)
	assert.Equal(t, charge, replayed)
}
