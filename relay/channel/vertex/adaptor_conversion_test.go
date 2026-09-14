package vertex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVertexResponsesConversionUsesSelectedNativeProtocol(t *testing.T) {
	for _, mode := range []int{RequestModeClaude, RequestModeGemini, RequestModeOpenSource} {
		c,_:=gin.CreateTestContext(httptest.NewRecorder())
		c.Request=httptest.NewRequest(http.MethodPost,"/v1/responses",nil)
		info:=&relaycommon.RelayInfo{ChannelMeta:&relaycommon.ChannelMeta{UpstreamModelName:"model-test"}}
		result,err:=(&Adaptor{RequestMode:mode}).ConvertOpenAIResponsesRequest(c,info,dto.OpenAIResponsesRequest{Model:"model-test",Input:[]byte(`"hello"`)})
		require.NoError(t,err)
		require.NotNil(t,result)
		if mode==RequestModeGemini { require.IsType(t,&dto.GeminiChatRequest{},result) }
		if mode==RequestModeOpenSource { require.IsType(t,&dto.GeneralOpenAIRequest{},result) }
		require.Equal(t,"model-test",c.GetString("request_model"))
	}
}
