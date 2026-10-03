package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetiredChannelHasNoProtocolOrEndpoint(t *testing.T) {
	apiType, registered := ChannelType2APIType(constant.ChannelTypeCodex)
	assert.False(t, registered)
	assert.Equal(t, -1, apiType, "retired credentials cannot fall back to OpenAI")
	assert.Empty(t, constant.GetChannelBaseURL(constant.ChannelTypeCodex))
	for _, name := range []string{"gpt-5-codex", "gpt-image-1", "literal-openai-compact"} {
		assert.Empty(t, GetEndpointTypesByChannelType(constant.ChannelTypeCodex, name))
	}
	definition := constant.GetChannelTypeDefinition(constant.ChannelTypeCodex)
	assert.Equal(t, constant.ChannelLifecycleRetired, definition.Lifecycle)
	assert.Equal(t, constant.ChannelCategoryReserved, definition.Category)
	require.ErrorIs(t, constant.ValidateChannelTypeLifecycle(57), constant.ErrRetiredChannelType)
	assert.Equal(t, []int{57}, constant.RetiredChannelTypes())
}

func TestActiveChannelProtocolRegistrationSurvivesRetirement(t *testing.T) {
	for _, test := range []struct {
		name                             string
		channelType, expectedID, apiType int
		category                         constant.ChannelCategory
		endpoint                         constant.EndpointType
	}{
		{"OpenAI", constant.ChannelTypeOpenAI, 1, constant.APITypeOpenAI, constant.ChannelCategoryOpenAICompatible, constant.EndpointTypeOpenAIResponse},
		{"Anthropic", constant.ChannelTypeAnthropic, 14, constant.APITypeAnthropic, constant.ChannelCategoryNativeProvider, constant.EndpointTypeAnthropic},
		{"Gemini", constant.ChannelTypeGemini, 24, constant.APITypeGemini, constant.ChannelCategoryNativeProvider, constant.EndpointTypeGemini},
		{"Sub2API", constant.ChannelTypeSub2API, 59, constant.APITypeSub2API, constant.ChannelCategoryGateway, constant.EndpointTypeOpenAIResponse},
		{"NewAPI", constant.ChannelTypeNewAPI, 60, constant.APITypeNewAPI, constant.ChannelCategoryGateway, constant.EndpointTypeOpenAIResponse},
		{"Advanced Custom", constant.ChannelTypeAdvancedCustom, 58, constant.APITypeAdvancedCustom, constant.ChannelCategoryLocal, constant.EndpointTypeOpenAI},
		{"vLLM", constant.ChannelTypeVLLM, 62, constant.APITypeAdvancedCustom, constant.ChannelCategoryLocal, constant.EndpointTypeOpenAI},
		{"SGLang", constant.ChannelTypeSGLang, 63, constant.APITypeAdvancedCustom, constant.ChannelCategoryLocal, constant.EndpointTypeOpenAI},
		{"Replicate", constant.ChannelTypeReplicate, 56, constant.APITypeReplicate, constant.ChannelCategoryMediaTask, constant.EndpointTypeOpenAI},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expectedID, test.channelType, "persisted channel identities never shift")
			apiType, registered := ChannelType2APIType(test.channelType)
			require.True(t, registered)
			assert.Equal(t, test.apiType, apiType)
			assert.Contains(t, GetEndpointTypesByChannelType(test.channelType, "gpt-5-codex"), test.endpoint)
			definition := constant.GetChannelTypeDefinition(test.channelType)
			assert.Equal(t, constant.ChannelLifecycleActive, definition.Lifecycle)
			assert.Equal(t, test.category, definition.Category)
		})
	}
	for _, name := range []string{"gpt-5-codex", "gpt-5.1-codex", "gpt-5.3-codex"} {
		assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact}, GetEndpointTypesByChannelType(constant.ChannelTypeOpenAI, name))
	}
}
