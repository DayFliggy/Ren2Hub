package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Audio ratios are the only built-in prices left, copied from NewAPI. The
// compatible relay bills audio tokens on the audio path only when one of
// these ratios exists, so losing them would bill audio at the text price.
func TestInitRatioSettingsSeedsOnlyUpstreamAudioRatios(t *testing.T) {
	savedAudio, savedCompletion := AudioRatio2JSONString(), AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateAudioRatioByJSONString(savedAudio))
		require.NoError(t, UpdateAudioCompletionRatioByJSONString(savedCompletion))
	})
	require.NoError(t, UpdateAudioRatioByJSONString(`{}`))
	require.NoError(t, UpdateAudioCompletionRatioByJSONString(`{}`))

	InitRatioSettings()

	require.True(t, ContainsAudioRatio("gpt-4o-audio-preview"))
	require.Equal(t, 16.0, GetAudioRatio("gpt-4o-audio-preview"))
	require.True(t, ContainsAudioCompletionRatio("tts-1"))
	require.Equal(t, 0.0, GetAudioCompletionRatio("tts-1"))
	_, priced, _ := GetModelRatio("gpt-4o-audio-preview")
	require.False(t, priced, "audio ratios must not price the model itself")

	defaults := GetDefaultPricingMaps()
	require.ElementsMatch(t, []string{"AudioRatio", "AudioCompletionRatio"}, keys(defaults))
	defaults["AudioRatio"]["gpt-4o-audio-preview"] = 1
	require.Equal(t, 16.0, GetDefaultPricingMaps()["AudioRatio"]["gpt-4o-audio-preview"], "callers get copies")
}

func keys(values map[string]map[string]float64) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
