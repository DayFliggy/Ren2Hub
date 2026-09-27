package helper

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBillingAttributesExcludeRawPayloadAndHeaders(t *testing.T) {
	attrs, err := BillingAttributesFromBody([]byte(`{"service_tier":"priority","messages":[{"content":"private"}],"password":"private","n":3,"size":"1024x1024"}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"service_tier": "priority", "quantity": json.Number("3"), "resolution": "1024x1024"}, attrs)
	input, err := BuildBillingExprRequestInputFromRequest(&dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: "private"}}}, map[string]string{"Authorization": "private"})
	require.NoError(t, err)
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
	assert.Equal(t, json.Number("2"), attrs["quantity"])
	assert.Equal(t, "b", attrs["resolution"])
}

func TestBillingAttributesCaptureFinalReasoningAndExplicitZero(t *testing.T) {
	attrs, err := BillingAttributesFromBody([]byte(`{"temperature":0,"top_p":0,"reasoning":{"effort":"high"}}`))
	require.NoError(t, err)
	assert.Equal(t, json.Number("0"), attrs["temperature"])
	assert.Equal(t, json.Number("0"), attrs["top_p"])
	assert.Equal(t, "high", attrs["effort"])
	assert.Equal(t, "on", attrs["thinking"])
	attrs, err = BillingAttributesFromBody([]byte(`{"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"thinking": "adaptive"}, attrs)
	attrs, err = BillingAttributesFromBody([]byte(`{"thinking":{"type":"enabled","budget_tokens":1024},"output_config":{"effort":"max"}}`))
	require.NoError(t, err)
	assert.Equal(t, json.Number("1024"), attrs["thinking_budget_tokens"])
	assert.NotContains(t, attrs, "temperature")
}

func TestFinalServiceTierPreservesAttributeAndWireBody(t *testing.T) {
	for _, tier := range []string{"priority", "fast", "ultrafast", "flex", " FAST ", "Unknown"} {
		t.Run(tier, func(t *testing.T) {
			body, err := common.Marshal(map[string]interface{}{"service_tier": tier})
			require.NoError(t, err)
			original := append([]byte(nil), body...)
			info := &relaycommon.RelayInfo{}
			require.NoError(t, FreezeOutboundBillingAttributes(info, body))
			assert.Equal(t, tier, info.BillingRequestInput.Attributes["service_tier"])
			assert.Equal(t, original, body)
		})
	}
	attrs, err := BillingAttributesFromBody([]byte(`{}`))
	require.NoError(t, err)
	assert.NotContains(t, attrs, "service_tier")
}
