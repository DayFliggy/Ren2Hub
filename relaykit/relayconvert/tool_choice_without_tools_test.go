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

// Upstreams reject tool_choice and parallel_tool_calls in a request without
// tools. When no function tool reaches the target (none was declared, or only
// a hosted search that the target carries outside its tool list), both
// controls are omitted with a warning (NewAPI 0a03088e0); strict rejects it.
func TestToolChoiceIsOmittedWhenNoToolReachesTheTarget(t *testing.T) {
	const responsesSearchOnly = `{"model":"m","input":"hi","tools":[{"type":"web_search"}],"tool_choice":"auto","parallel_tool_calls":false}`
	const responsesSearchRequired = `{"model":"gemini-3-pro","input":"hi","tools":[{"type":"web_search"}],"tool_choice":"required"}`
	const chatWithoutTools = `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"tool_choice":"auto","parallel_tool_calls":false}`
	cases := []struct {
		name    string
		body    string
		chat    bool
		target  types.RelayFormat
		omitted []string
		kept    string
	}{
		{name: "responses search to chat", body: responsesSearchOnly, target: types.RelayFormatOpenAI, omitted: []string{`"tool_choice"`, `"parallel_tool_calls"`}, kept: `"web_search_options"`},
		{name: "responses search to gemini", body: responsesSearchRequired, target: types.RelayFormatGemini, omitted: []string{`"toolConfig"`, `"functionCallingConfig"`}, kept: `"googleSearch"`},
		{name: "chat without tools to claude", body: chatWithoutTools, chat: true, target: types.RelayFormatClaude, omitted: []string{`"tool_choice"`, `"disable_parallel_tool_use"`}},
		{name: "chat without tools to responses", body: chatWithoutTools, chat: true, target: types.RelayFormatOpenAIResponses, omitted: []string{`"tool_choice"`, `"parallel_tool_calls"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decode := func() any {
				if tc.chat {
					request := &dto.GeneralOpenAIRequest{}
					require.NoError(t, kitutil.Unmarshal([]byte(tc.body), request))
					return request
				}
				request := &dto.OpenAIResponsesRequest{}
				require.NoError(t, kitutil.Unmarshal([]byte(tc.body), request))
				return request
			}
			info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicySafe}, UpstreamModelName: "m", ChannelMetaAttached: true}
			result, err := ConvertRequest(context.Background(), info, tc.target, decode())
			require.NoError(t, err)
			encoded := marshaledField(t, result.Value)
			for _, field := range tc.omitted {
				assert.NotContains(t, encoded, field)
			}
			if tc.kept != "" {
				assert.Contains(t, encoded, tc.kept)
			}
			diagnostic := findDiagnostic(result.Diagnostics, "tool_choice_without_tools")
			require.NotNil(t, diagnostic)
			assert.Equal(t, types.ConversionDiagnosticWarning, diagnostic.Severity)

			info.Options.ConversionPolicy = types.ConversionLossPolicyStrict
			_, err = ConvertRequest(context.Background(), info, tc.target, decode())
			var loss *types.ConversionLossError
			require.ErrorAs(t, err, &loss)
			assert.NotNil(t, findDiagnostic(loss.Diagnostics, "tool_choice_without_tools"))
		})
	}
}
