package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatResponsesEOFDoesNotManufactureCompletion(t *testing.T) {
	recorder:=httptest.NewRecorder()
	c,_:=gin.CreateTestContext(recorder)
	c.Request=httptest.NewRequest(http.MethodPost,"/v1/responses",nil)
	info:=&relaycommon.RelayInfo{RelayFormat:types.RelayFormatOpenAIResponses,DisablePing:true,ChannelMeta:&relaycommon.ChannelMeta{UpstreamModelName:"gpt-test"}}
	body:=`data: {"id":"chatcmpl_1","choices":[{"index":0,"delta":{"content":"partial"}}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`+"\n\n"
	usage,apiErr:=OaiChatToResponsesStreamHandler(c,info,&http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(body))})
	require.NotNil(t,apiErr)
	require.NotNil(t,usage)
	assert.Equal(t,5,usage.TotalTokens)
	assert.Contains(t,recorder.Body.String(),"response.failed")
	assert.NotContains(t,recorder.Body.String(),"response.completed")
}

func TestBufferedResponsesEOFDoesNotManufactureSuccess(t *testing.T) {
	recorder:=httptest.NewRecorder()
	c,_:=gin.CreateTestContext(recorder)
	c.Request=httptest.NewRequest(http.MethodPost,"/v1/chat/completions",nil)
	info:=&relaycommon.RelayInfo{RelayFormat:types.RelayFormatOpenAI,ChannelMeta:&relaycommon.ChannelMeta{UpstreamModelName:"gpt-test"}}
	body:="data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\",\"output_index\":0,\"content_index\":0}\n\n"
	usage,apiErr:=OaiResponsesToChatBufferedStreamHandler(c,info,&http.Response{StatusCode:200,Header:make(http.Header),Body:io.NopCloser(strings.NewReader(body))})
	require.NotNil(t,apiErr)
	require.NotNil(t,usage)
	assert.Greater(t,usage.CompletionTokens,0)
	assert.Empty(t,recorder.Body.String())
	assert.False(t,c.Writer.Written())
}
