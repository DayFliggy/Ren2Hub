package ratio_setting

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompletionRatioMetadataUsesConfiguredWithoutDefaults(t *testing.T) {
	previous := CompletionRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, UpdateCompletionRatioByJSONString(previous)) })
	require.NoError(t, UpdateCompletionRatioByJSONString(`{"gpt-5":7,"fixture-model":2.5}`))
	require.Equal(t, CompletionRatioInfo{Ratio: 7}, GetCompletionRatioInfo("gpt-5"))
	require.Equal(t, CompletionRatioInfo{Ratio: 2.5}, GetCompletionRatioInfo("fixture-model"))
	require.Equal(t, CompletionRatioInfo{Ratio: 1}, GetCompletionRatioInfo("gpt-4o"))
	draft := 3.0
	require.Equal(t, CompletionRatioInfo{Ratio: 3}, ResolveCompletionRatio("gpt-5", &draft))
	require.Equal(t, CompletionRatioInfo{Ratio: 1}, ResolveCompletionRatio("gpt-5", nil))
}
