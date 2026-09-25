package helper

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBillingAttributesExcludeRawPayloadAndHeaders(t *testing.T) {
	attrs, err := BillingAttributesFromBody([]byte(`{"service_tier":"priority","messages":[{"content":"private"}],"password":"private","n":3,"size":"1024x1024"}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"service_tier": "priority", "quantity": float64(3), "resolution": "1024x1024"}, attrs)
	input, err := BuildBillingExprRequestInputFromRequest(&dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: "private"}}}, map[string]string{"Authorization": "private"})
	require.NoError(t, err)
	assert.Empty(t, input.Body)
	assert.Empty(t, input.Headers)
	encoded, err := common.Marshal(input)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private")
}

func TestBillingAttributesRejectInvalidScalarAndPreferCanonicalFields(t *testing.T) {
	for _, body := range []string{`{"quantity":-1}`, `{"quantity":1e100}`, `{"quality":{"secret":"x"}}`} {
		_, err := BillingAttributesFromBody([]byte(body))
		require.Error(t, err)
	}
	attrs, err := BillingAttributesFromBody([]byte(`{"n":1,"quantity":2,"size":"a","resolution":"b"}`))
	require.NoError(t, err)
	assert.Equal(t, float64(2), attrs["quantity"])
	assert.Equal(t, "b", attrs["resolution"])
}

func TestBillingAttributesCaptureFinalReasoningAndExplicitZero(t *testing.T) {
	attrs, err := BillingAttributesFromBody([]byte(`{"temperature":0,"top_p":0,"reasoning":{"effort":"high"}}`))
	require.NoError(t, err)
	assert.Equal(t, float64(0), attrs["temperature"])
	assert.Equal(t, float64(0), attrs["top_p"])
	assert.Equal(t, "high", attrs["effort"])
	assert.Equal(t, "on", attrs["thinking"])
	attrs, err = BillingAttributesFromBody([]byte(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"thinking": "adaptive"}, attrs)
	attrs, err = BillingAttributesFromBody([]byte(`{"thinking":{"type":"enabled","budget_tokens":1024},"output_config":{"effort":"max"}}`))
	require.NoError(t, err)
	assert.Equal(t, float64(1024), attrs["thinking_budget_tokens"])
	assert.NotContains(t, attrs, "temperature")
}
