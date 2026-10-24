package constant

import (
	"testing"

	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGeminiModelActionDispatch(t *testing.T) {
	for _, version := range []string{"v1", "v1beta"} {
		for _, action := range []string{"generateContent", "streamGenerateContent", "embedContent", "batchEmbedContents", "predict", "countTokens"} {
			path := "/" + version + "/models/gemini-2.5-flash:" + action
			model, parsedAction, err := ParseGeminiModelAction(path)
			require.NoError(t, err)
			require.Equal(t, "gemini-2.5-flash", model)
			require.Equal(t, action, parsedAction)
			if action == "countTokens" {
				require.Equal(t, appconstant.ChannelTypeGemini, NativeCountChannelType(path))
				require.Equal(t, RelayModeNativeCount, Path2RelayMode(path))
			} else {
				require.Zero(t, NativeCountChannelType(path))
				require.Equal(t, RelayModeGemini, Path2RelayMode(path))
			}
		}
		for _, suffix := range []string{"gemini:unknown", "gemini", ":countTokens", "../gemini:countTokens", "gemini%2fother:countTokens", "gemini%252fother:countTokens", "gemini?other:countTokens", "gemini#other:countTokens", "gemini:countTokens:generateContent", "gemini:countTokens/"} {
			_, _, err := ParseGeminiModelAction("/" + version + "/models/" + suffix)
			require.Error(t, err, suffix)
		}
	}
	require.Equal(t, appconstant.ChannelTypeAnthropic, NativeCountChannelType("/v1/messages/count_tokens"))
	require.Equal(t, RelayModeNativeCount, Path2RelayMode("/v1/messages/count_tokens"))
	require.Zero(t, NativeCountChannelType("/v1/messages"))
}
