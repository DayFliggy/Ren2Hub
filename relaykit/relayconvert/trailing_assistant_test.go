package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In Responses, assistant output at the end of input is history; Claude and
// Gemini would continue it as a prefill, which newer models reject. The
// converters drop trailing assistant text after the last user turn or tool
// result (NewAPI e0483122c) and keep input that ends in a pending tool call or
// holds only assistant turns.
func TestResponsesTrailingAssistantTextIsOmittedForClaudeAndGemini(t *testing.T) {
	const call = `{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}`
	const tools = `"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`
	const answer = `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"retried answer"}]}`
	cases := []struct {
		name    string
		input   string
		omitted bool
	}{
		{name: "after user turn", input: `[{"role":"user","content":"hi"},` + answer + `]`, omitted: true},
		{name: "after tool result", input: `[{"role":"user","content":"hi"},` + call + `,{"type":"function_call_output","call_id":"call_1","output":"ok"},` + answer + `]`, omitted: true},
		{name: "pending tool call", input: `[{"role":"user","content":"hi"},` + answer + `,` + call + `]`},
		{name: "only assistant turns", input: `[` + answer + `]`},
	}
	for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, tc := range cases {
			t.Run(string(target)+"/"+tc.name, func(t *testing.T) {
				var req dto.OpenAIResponsesRequest
				require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test-model","max_output_tokens":64,"input":`+tc.input+`,`+tools+`}`), &req))
				info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicySafe}, UpstreamModelName: "test-model", ChannelMetaAttached: true}

				result, err := ConvertRequest(context.Background(), info, target, &req)
				require.NoError(t, err)
				encoded, err := kitutil.Marshal(result.Value)
				require.NoError(t, err)
				diagnostic := findDiagnostic(result.Diagnostics, "trailing_assistant_omitted")
				if !tc.omitted {
					assert.Contains(t, string(encoded), "retried answer")
					assert.Nil(t, diagnostic)
					return
				}
				assert.NotContains(t, string(encoded), "retried answer")
				require.NotNil(t, diagnostic)
				assert.Equal(t, types.ConversionDiagnosticWarning, diagnostic.Severity)
				assert.Equal(t, "user", lastTurnRole(t, result.Value))

				info.Options.ConversionPolicy = types.ConversionLossPolicyStrict
				_, err = ConvertRequest(context.Background(), info, target, &req)
				var loss *types.ConversionLossError
				require.ErrorAs(t, err, &loss)
				assert.NotNil(t, findDiagnostic(loss.Diagnostics, "trailing_assistant_omitted"))
			})
		}
	}
}

func lastTurnRole(t *testing.T, value any) string {
	t.Helper()
	switch request := value.(type) {
	case *dto.ClaudeRequest:
		require.NotEmpty(t, request.Messages)
		return request.Messages[len(request.Messages)-1].Role
	case *dto.GeminiChatRequest:
		require.NotEmpty(t, request.Contents)
		return request.Contents[len(request.Contents)-1].Role
	}
	t.Fatalf("unexpected converted request %T", value)
	return ""
}
