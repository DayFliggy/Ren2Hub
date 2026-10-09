package common

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
)

func TestRelayInfoConvOptionsUsesNormalizedGeminiSafetySettings(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	original := settings.SafetySettings
	t.Cleanup(func() {
		settings.SafetySettings = original
	})
	settings.SafetySettings = map[string]string{
		"HARM_CATEGORY_HATE_SPEECH":       "",
		"HARM_CATEGORY_DANGEROUS_CONTENT": "BLOCK_ONLY_HIGH",
	}

	options := (&RelayInfo{}).ConvOptions()

	assert.Equal(t, "OFF", options.Gemini.SafetySetting("HARM_CATEGORY_HATE_SPEECH"))
	assert.Equal(t, "BLOCK_ONLY_HIGH", options.Gemini.SafetySetting("HARM_CATEGORY_DANGEROUS_CONTENT"))
}

// The thought-signature bypass is on by default, as in NewAPI, and the
// setting reaches the converters on every request snapshot.
func TestRelayInfoConvOptionsCarriesGeminiThoughtSignatureSetting(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	original := settings.FunctionCallThoughtSignatureEnabled
	t.Cleanup(func() {
		settings.FunctionCallThoughtSignatureEnabled = original
	})

	assert.True(t, original, "the bypass is enabled by default")
	assert.True(t, (&RelayInfo{}).ConvOptions().Gemini.FunctionCallThoughtSignatureEnabled)

	settings.FunctionCallThoughtSignatureEnabled = false
	assert.False(t, (&RelayInfo{}).ConvOptions().Gemini.FunctionCallThoughtSignatureEnabled)
}
