package helper

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalAudioPriceUsesProviderText(t *testing.T) {
	for _, test := range []struct {
		name, body string
		api        int
		valid      bool
	}{
		{"openai", `{"input":"Hello"}`, constant.APITypeOpenAI, true},
		{"minimax", `{"text":"Hello"}`, constant.APITypeMiniMax, true},
		{"volc", `{"request":{"text":"Hello"}}`, constant.APITypeVolcEngine, true},
		{"empty", `{"input":" "}`, constant.APITypeOpenAI, false},
		{"null", `{"text":null}`, constant.APITypeMiniMax, false},
		{"missing_nested", `{"input":"ignored"}`, constant.APITypeVolcEngine, false},
		{"invalid_nested", `{"request":{"text":5}}`, constant.APITypeVolcEngine, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "tts-1", ChannelMeta: &relaycommon.ChannelMeta{ApiType: test.api}}
			meta, tokens, err := FinalAudioPriceMeta(info, strings.NewReader(test.body))
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "Hello", meta.CombineText)
			assert.Equal(t, 5, tokens)
		})
	}
}
