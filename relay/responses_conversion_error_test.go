package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareResponsesConversionLossIsBadRequestBeforeSend(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previousPassThrough })
	for _, test := range []struct {
		name   string
		policy types.ConversionLossPolicy
		tools  string
	}{
		{"strict freeform", types.ConversionLossPolicyStrict, `[{"type":"custom","name":"shell","format":{"type":"text"}}]`},
		{"safe grammar", types.ConversionLossPolicySafe, `[{"type":"custom","name":"shell","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-test")
			request := &dto.OpenAIResponsesRequest{Model: "claude-test", Input: []byte(`"hello"`), MaxOutputTokens: common.GetPointer(uint(64)), Tools: []byte(test.tools)}
			info := &relaycommon.RelayInfo{OriginModelName: request.Model, Request: request, RelayFormat: types.RelayFormatOpenAIResponses, ConversionPolicy: test.policy}
			adaptor, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Equal(t, types.ErrorCodeConvertRequestFailed, apiErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.Nil(t, adaptor)
			assert.Nil(t, body)
			assert.Nil(t, closer)
			assert.False(t, info.UpstreamSent)
			assert.False(t, info.HasValidOutput())
			assert.NotEmpty(t, info.ConversionDiagnostics)
			assert.Empty(t, recorder.Body.String())
		})
	}
}
