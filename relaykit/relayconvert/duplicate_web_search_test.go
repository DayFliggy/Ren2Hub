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

// Chat Completions carries one hosted web search. A second definition is a
// tool loss the policy decides on (NewAPI acb347a19): safe and strict reject
// it as a conversion loss, which the host answers with 400, and allow keeps
// the first definition and drops the rest. It is never an internal error.
func TestDuplicateHostedWebSearchToChatIsAPolicyDecision(t *testing.T) {
	const body = `{"model":"m","input":"hi","tools":[{"type":"web_search","search_context_size":"low"},{"type":"web_search_preview","search_context_size":"high"},{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`
	decode := func() *dto.OpenAIResponsesRequest {
		request := &dto.OpenAIResponsesRequest{}
		require.NoError(t, kitutil.Unmarshal([]byte(body), request))
		return request
	}
	info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicySafe}, UpstreamModelName: "m", ChannelMetaAttached: true}

	_, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, decode())
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	diagnostic := findDiagnostic(loss.Diagnostics, "duplicate_hosted_web_search")
	require.NotNil(t, diagnostic)
	assert.Equal(t, "tools[1]", diagnostic.Path)

	info.Options.ConversionPolicy = types.ConversionLossPolicyAllow
	result, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, decode())
	require.NoError(t, err)
	chat, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, chat.WebSearchOptions)
	assert.Equal(t, "low", chat.WebSearchOptions.SearchContextSize)
	require.Len(t, chat.Tools, 1)
	assert.Equal(t, "lookup", chat.Tools[0].Function.Name)
	assert.NotNil(t, findDiagnostic(result.Diagnostics, "duplicate_hosted_web_search"))
}
