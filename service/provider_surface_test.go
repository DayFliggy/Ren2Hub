package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProviderSurfacePlansMatchNewNativeEndpoints(t *testing.T) {
	for _, test := range []struct {
		kind    int
		path    string
		request any
	}{
		{constant.ChannelTypeOllama, "/v1/messages", &dto.ClaudeRequest{Model: "llama", MaxTokens: func() *uint { v := uint(128); return &v }()}},
		{constant.ChannelTypeOllama, "/v1/responses", &dto.OpenAIResponsesRequest{Model: "llama", Input: []byte(`"hi"`)}},
		{constant.ChannelTypeZhipu_v4, "/v1/responses", &dto.OpenAIResponsesRequest{Model: "glm", Input: []byte(`"hi"`)}},
		{constant.ChannelTypeVLLM, "/v1/chat/completions", &dto.GeneralOpenAIRequest{Model: "model"}},
		{constant.ChannelTypeSGLang, "/v1/rerank", &dto.RerankRequest{Model: "model", Query: "q", Documents: []any{"d"}}},
	} {
		requirements, err := relayconvert.AnalyzeRequest(test.request)
		require.NoError(t, err)
		ch := &model.Channel{Type: test.kind}
		require.NoError(t, ch.ValidateSettings())
		plan, err := PlanChannelConversion(requirements, ch, test.path, requirements.Model, types.ConversionLossPolicySafe)
		require.NoError(t, err)
		assert.True(t, plan.Native)
	}
}

func TestOllamaChatModeControlsProjection(t *testing.T) {
	req, err := relayconvert.AnalyzeRequest(&dto.GeneralOpenAIRequest{Model: "llama", ToolChoice: "required"})
	require.NoError(t, err)
	native := &model.Channel{Type: constant.ChannelTypeOllama}
	_, err = PlanChannelConversion(req, native, "/v1/chat/completions", "llama", types.ConversionLossPolicySafe)
	require.Error(t, err)
	native.OtherSettings = `{"ollama_openai_chat":true}`
	plan, err := PlanChannelConversion(req, native, "/v1/chat/completions", "llama", types.ConversionLossPolicySafe)
	require.NoError(t, err)
	assert.True(t, plan.Native)
}
