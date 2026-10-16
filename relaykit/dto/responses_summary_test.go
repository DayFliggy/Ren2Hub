package dto

import (
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReasoningSummaryAlwaysUsesJSONArray(t *testing.T) {
	for _, summary := range [][]ResponsesReasoningSummaryPart{nil, {}, {{Type: "summary_text", Text: "reason"}}} {
		data, err := kitutil.Marshal(ResponsesOutput{Type: "reasoning", Summary: summary})
		require.NoError(t, err)
		if len(summary) == 0 {
			require.Contains(t, string(data), `"summary":[]`)
		} else {
			require.Contains(t, string(data), `"text":"reason"`)
		}
	}
	data, err := kitutil.Marshal(ResponsesOutput{Type: "message"})
	require.NoError(t, err)
	require.NotContains(t, string(data), `"summary"`)
}
