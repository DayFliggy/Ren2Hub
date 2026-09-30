package billing

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedActualExpressionPreservesItsEncryptedInputs(t *testing.T) {
	version, err := NewModelPriceVersion("failed-actual", PricingValues{
		"billing_setting.billing_mode": "tiered_expr",
		"billing_setting.billing_expr": `len < 10 ? p : (hour("UTC") >= 0 ? p * param("actual_price") : p)`,
	}, false)
	require.NoError(t, err)
	quote, err := NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	cipher, err := NewEvidenceCipher([]byte("failed-evaluation-test-key"))
	require.NoError(t, err)
	binding := EvidenceBinding{RequestID: "request", AttemptID: "attempt", ConfigHash: PricingConfigHash(quote), Phase: "settlement"}
	input := billingexpr.RequestInput{Body: []byte(`{"actual_price":-1,"private":"actual-private"}`), Headers: map[string]string{"Authorization": "secret-actual"}}
	usage, charge, err := cipher.Evaluate(quote, Usage{InputTokens: 20, Complete: true}, input, binding)
	require.Error(t, err)
	require.Nil(t, charge)
	require.NotNil(t, usage.Evidence)
	assert.EqualValues(t, 20, usage.InputTokens)
	assert.Nil(t, usage.requestInput)
	encoded, err := common.Marshal(usage)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "actual-private")
	assert.NotContains(t, string(encoded), "secret-actual")
	aad, err := evidenceAAD(quote, binding)
	require.NoError(t, err)
	plaintext, err := cipher.aead.Open(nil, usage.Evidence.Nonce, usage.Evidence.Ciphertext, aad)
	require.NoError(t, err)
	defer clear(plaintext)
	var evidence evaluationEvidence
	require.NoError(t, common.Unmarshal(plaintext, &evidence))
	assert.Equal(t, input.Body, evidence.Input.Body)
	assert.Equal(t, input.Headers, evidence.Input.Headers)
	require.Len(t, evidence.Calls, 1)
	assert.Equal(t, "hour", evidence.Calls[0].Name)
	_, err = cipher.Replay(quote, usage, binding)
	require.ErrorIs(t, err, ErrEvaluationEvidence)
	usage.Evidence.Ciphertext[0] ^= 1
	_, err = cipher.Replay(quote, usage, binding)
	require.ErrorIs(t, err, ErrEvaluationEvidence)
}
