package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureConvertedTextTargetsChatDeployment(t *testing.T) {
	for _, input := range []struct {
		format types.RelayFormat
		path   string
	}{
		{types.RelayFormatClaude, "/v1/messages?beta=true"},
		{types.RelayFormatGemini, "/v1beta/models/gemini-test:streamGenerateContent?alt=sse"},
	} {
		t.Run(string(input.format), func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayFormat: input.format, RelayMode: relayconstant.RelayModeChatCompletions, RequestURLPath: input.path,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAzure, ChannelBaseUrl: "https://azure.example.invalid", UpstreamModelName: "deployment", ApiVersion: "test-version"}}
			url, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://azure.example.invalid/openai/deployments/deployment/chat/completions?api-version=test-version", url)
		})
	}
}
