package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutboundBudgetUsesFinalWirePayload(t *testing.T) {
	oldCount := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = oldCount })
	for _, test := range []struct {
		name, body string
		api        int
		format     types.RelayFormat
		output     int
	}{
		{"chat", `{"messages":[{"role":"system","content":"channel policy"},{"role":"user","content":"hello"}],"max_tokens":80,"max_completion_tokens":20,"n":3}`, constant.APITypeOpenAI, types.RelayFormatOpenAI, 60},
		{"responses", `{"instructions":"channel policy","input":"hello","max_output_tokens":22}`, constant.APITypeOpenAI, types.RelayFormatOpenAIResponses, 22},
		{"claude", `{"system":"channel policy","messages":[{"role":"user","content":"hello"}],"max_tokens":24}`, constant.APITypeAnthropic, types.RelayFormatClaude, 24},
		{"gemini", `{"system_instruction":{"parts":[{"text":"channel policy"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"max_output_tokens":26,"candidate_count":2}}`, constant.APITypeGemini, types.RelayFormatGemini, 52},
		{"nova", `{"system":[{"text":"channel policy"}],"messages":[{"role":"user","content":[{"text":"hello"}]}],"inferenceConfig":{"maxTokens":28}}`, constant.APITypeAws, types.RelayFormatOpenAI, 28},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: test.api, UpstreamModelName: "nova-lite"}, RelayFormat: test.format}
			budget, err := AnalyzeOutboundTokenBudget(info, strings.NewReader(test.body))
			require.NoError(t, err)
			require.NotNil(t, budget)
			assert.True(t, budget.Bounded, budget.Reason)
			assert.Positive(t, budget.InputTokens, "admission cannot be disabled by CountToken")
			assert.Equal(t, test.output, budget.OutputTokens)
			larger, err := AnalyzeOutboundTokenBudget(info, strings.NewReader(strings.ReplaceAll(test.body, "channel policy", strings.Repeat("channel policy ", 40))))
			require.NoError(t, err)
			assert.Greater(t, larger.InputTokens, budget.InputTokens)
		})
	}
}

func TestOutboundBudgetRejectsInvalidLimitsAndOpaqueInputs(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI, UpstreamModelName: "gpt-4o-mini"}, RelayFormat: types.RelayFormatOpenAI}
	for _, fields := range []string{`"max_tokens":-1`, `"max_tokens":18446744073709551615`, `"max_tokens":10,"n":0`, `"max_tokens":10,"n":-1`, fmt.Sprintf(`"max_tokens":%d,"n":2`, maxTokensLimit)} {
		_, err := AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],`+fields+`}`))
		require.Error(t, err, fields)
	}
	for _, fields := range []string{`"max_tokens":0`, `"max_tokens":50,"max_completion_tokens":0`, `"max_tokens":50,"unknown":false`} {
		budget, err := AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],`+fields+`}`))
		require.NoError(t, err)
		assert.False(t, budget.Bounded)
	}
	info.RelayFormat = types.RelayFormatOpenAIResponses
	budget, err := AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"previous_response_id":"opaque","input":"hello","max_output_tokens":50}`))
	require.NoError(t, err)
	assert.False(t, budget.Bounded)
	assert.False(t, budget.InputKnown)
	info.ApiType, info.RelayFormat = constant.APITypeAnthropic, types.RelayFormatClaude
	budget, err = AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AA=="}}]}],"max_tokens":50}`))
	require.NoError(t, err)
	assert.False(t, budget.Bounded)
	assert.False(t, budget.InputKnown)
	info.ApiType, info.RelayFormat = constant.APITypeGemini, types.RelayFormatGemini
	budget, err = AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":50,"responseModalities":["IMAGE"]}}`))
	require.NoError(t, err)
	assert.False(t, budget.Bounded)
	assert.Equal(t, "non_text_output_unbounded", budget.Reason)
}

func TestOutboundBudgetUsesFinalProtocolAfterNativeEndpointSwitch(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeAli, UpstreamModelName: "qwen-test"},
		RelayFormat: types.RelayFormatClaude, FinalRequestRelayFormat: types.RelayFormatClaude}
	budget, err := AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"system":"channel policy","messages":[{"role":"user","content":"hello"}],"max_tokens":40}`))
	require.NoError(t, err)
	require.NotNil(t, budget)
	assert.True(t, budget.Bounded, budget.Reason)
	assert.Equal(t, 40, budget.OutputTokens)
	info.ApiType, info.RelayFormat, info.FinalRequestRelayFormat = constant.APITypeOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIResponses
	budget, err = AnalyzeOutboundTokenBudget(info, strings.NewReader(`{"instructions":"channel policy","input":"hello","max_output_tokens":60}`))
	require.NoError(t, err)
	assert.True(t, budget.Bounded, budget.Reason)
	assert.Equal(t, 60, budget.OutputTokens)
}
