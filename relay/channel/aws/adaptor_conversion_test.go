package aws

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestAWSConvertsResponsesAndGeminiToClaude(t *testing.T) {
	info:=&relaycommon.RelayInfo{ChannelMeta:&relaycommon.ChannelMeta{UpstreamModelName:"claude-test"}}
	responses,err:=(&Adaptor{}).ConvertOpenAIResponsesRequest(nil,info,dto.OpenAIResponsesRequest{Model:"claude-test",Input:[]byte(`"hello"`)})
	require.NoError(t,err)
	require.IsType(t,&dto.ClaudeRequest{},responses)
	gemini,err:=(&Adaptor{}).ConvertGeminiRequest(nil,info,&dto.GeminiChatRequest{Contents:[]dto.GeminiChatContent{{Role:"user",Parts:[]dto.GeminiPart{{Text:"hello"}}}}})
	require.NoError(t,err)
	require.IsType(t,&dto.ClaudeRequest{},gemini)
	require.Len(t,gemini.(*dto.ClaudeRequest).Messages,1)
}
