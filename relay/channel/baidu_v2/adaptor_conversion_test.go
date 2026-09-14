package baidu_v2

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeConversionUsesBaiduChatRouteAndSearchOverride(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"ernie-search","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`), &request))
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, RequestURLPath: "/v1/messages",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://baidu.example.invalid", UpstreamModelName: "ernie-search"}}
	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertClaudeRequest(nil, info, &request)
	require.NoError(t, err)
	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://baidu.example.invalid/v2/chat/completions", url)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(encoded, &body))
	assert.Equal(t, "ernie", body["model"])
	assert.Equal(t, true, body["web_search"].(map[string]any)["enable"])
	assert.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.RelayFormat)
}
