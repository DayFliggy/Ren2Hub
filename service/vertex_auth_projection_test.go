package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexOpenSourceAuthenticationProjection(t *testing.T) {
	apiKeySettings := `{"vertex_key_type":"api_key"}`
	channel := &model.Channel{Type: constant.ChannelTypeVertexAi, Models: "alias", OtherSettings: apiKeySettings}
	for _, actual := range []string{"meta/llama-3.3", "deepseek-r1-maas"} {
		for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
			requirements := relayconvert.RequestRequirements{Format: format, Model: "alias", Operation: "text"}
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				_, err := PlanChannelConversion(requirements, channel, "/v1/chat/completions", actual, policy)
				require.Error(t, err)
			}
		}
		endpoints := endpointTypesForModel(channel, modellab.ModelMatch{RealModel: actual})
		for _, endpoint := range []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini} {
			assert.NotContains(t, endpoints, endpoint)
		}
	}
	apiKeyFingerprint, err := channelCapabilityFingerprint(channel, nil)
	require.NoError(t, err)
	jsonSettings := `{"vertex_key_type":"json"}`
	channel.OtherSettings = jsonSettings
	jsonFingerprint, err := channelCapabilityFingerprint(channel, nil)
	require.NoError(t, err)
	assert.NotEqual(t, apiKeyFingerprint, jsonFingerprint, "auth mode changes must rebuild capability snapshots")
	_, err = PlanChannelConversion(relayconvert.RequestRequirements{Format: types.RelayFormatOpenAI, Model: "alias", Operation: "text"}, channel, "/v1/chat/completions", "llama", types.ConversionLossPolicySafe)
	require.NoError(t, err)
}
