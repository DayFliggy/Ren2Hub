package helper

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeEffortFinalBillingAttributes(t *testing.T) {
	for _, tc := range []struct {
		name, body, effort, thinking string
	}{
		{"message_override", `{"output_config":{"effort":"medium"},"messages":[{"role":"system","content":[],"output_config":{"effort":"HIGH"}},{"role":"user","content":"hi"}]}`, "high", "on"},
		{"pending", `{"output_config":{"effort":"medium"},"messages":[{"role":"user","content":"hi"},{"role":"system","content":[],"output_config":{"effort":"high"}}]}`, "medium", "on"},
		{"absent", `{"messages":[{"role":"user","content":"hi"}]}`, "", ""},
		{"disabled", `{"thinking":{"type":"disabled"},"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user","content":"hi"}]}`, "high", "off"},
		{"adaptive", `{"thinking":{"type":"adaptive"},"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user","content":"hi"}]}`, "high", "adaptive"},
		{"zero_budget", `{"thinking":{"budget_tokens":0},"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user","content":"hi"}]}`, "high", "off"},
		{"none", `{"output_config":{"effort":"medium"},"messages":[{"role":"system","output_config":{"effort":"none"}},{"role":"user","content":"hi"}]}`, "none", "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ReasoningEffort: "stale"}
			require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(tc.body)))
			assert.Equal(t, tc.effort, info.ReasoningEffort)
			if tc.effort == "" {
				assert.NotContains(t, info.BillingRequestInput.Attributes, "effort")
			} else {
				assert.Equal(t, tc.effort, info.BillingRequestInput.Attributes["effort"])
			}
			if tc.thinking == "" {
				assert.NotContains(t, info.BillingRequestInput.Attributes, "thinking")
			} else {
				assert.Equal(t, tc.thinking, info.BillingRequestInput.Attributes["thinking"])
			}
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &request))
			incoming, err := BuildBillingExprRequestInputFromRequest(&request, nil)
			require.NoError(t, err)
			assert.Equal(t, info.BillingRequestInput.Attributes, incoming.Attributes)
		})
	}
}

func TestClaudeEffortBillingUsesFinalProtocol(t *testing.T) {
	body := []byte(`{"reasoning_effort":"low","messages":[{"role":"system","content":[],"output_config":{"effort":"high"}},{"role":"user","content":"hi"}]}`)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, FinalRequestRelayFormat: types.RelayFormatOpenAI}
	require.NoError(t, FreezeOutboundBillingAttributes(info, body))
	assert.Equal(t, "low", info.BillingRequestInput.Attributes["effort"])
	assert.Equal(t, "low", info.ReasoningEffort)
	info.FinalRequestRelayFormat = types.RelayFormatClaude
	require.NoError(t, FreezeOutboundBillingAttributes(info, body))
	assert.Equal(t, "high", info.BillingRequestInput.Attributes["effort"])
	assert.Equal(t, "high", info.ReasoningEffort)
	info.FinalRequestRelayFormat = types.RelayFormatOpenAI
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)))
	assert.NotContains(t, info.BillingRequestInput.Attributes, "effort")
	assert.Empty(t, info.ReasoningEffort)
}

func TestClaudeEffortBillingRejectsMalformedConfiguration(t *testing.T) {
	for _, body := range []string{
		`{"output_config":{"effort":false}}`,
		`{"messages":[{"role":"system","output_config":{"effort":null}},{"role":"user"}]}`,
		`{"messages":[{"role":"assistant","output_config":{"effort":"high"}},{"role":"user"}]}`,
	} {
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}
		require.Error(t, FreezeOutboundBillingAttributes(info, []byte(body)))
		assert.Nil(t, info.BillingRequestInput)
	}
}

func TestClaudeEffortRetryDoesNotRewriteFrozenQuote(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"output_config":{"effort":"medium"}}`)))
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: `v1:p * (attr("effort") == "high" ? 10 : 1)`}, "1", info.BillingRequestInput.Attributes, time.Now().UTC())
	require.NoError(t, err)
	before, err := common.Marshal(quote)
	require.NoError(t, err)
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"}]}`)))
	assert.Equal(t, "high", info.ReasoningEffort)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 1000, ContextTokens: 1000, InputIncludesCache: true})
	require.NoError(t, err)
	assert.Equal(t, "0.001", charge.TotalUSD)
	after, err := common.Marshal(quote)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"messages":[{"role":"user"}]}`)))
	assert.Empty(t, info.ReasoningEffort)
	assert.NotContains(t, info.BillingRequestInput.Attributes, "effort")
}
