package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDifyProjectionPolicyMatchesPlanning(t *testing.T) {
	for _, sample := range []struct {
		name, body string
		severity   string
	}{
		{"text", `{"messages":[{"role":"user","content":"text"}]}`, "none"},
		{"image", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://must-not-fetch.invalid/image"}}]}]}`, "none"},
		{"image_detail", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://must-not-fetch.invalid/image","detail":"low"}}]}]}`, "warning"},
		{"history", `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"next"}]}`, "error"},
		{"zero_output_limit", `{"max_tokens":0}`, "error"},
		{"tool_control", `{"parallel_tool_calls":false}`, "error"},
	} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
			t.Run(sample.name+"/"+string(policy), func(t *testing.T) {
				request := &dto.GeneralOpenAIRequest{Model: "dify", Messages: []dto.Message{{Role: "user", Content: "text"}}}
				require.NoError(t, common.Unmarshal([]byte(sample.body), request))
				info := &relaycommon.RelayInfo{Request: request, RelayFormat: types.RelayFormatOpenAI, ConversionPolicy: policy}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				planning := PlanRequestForChannel(c, info, &model.Channel{Type: constant.ChannelTypeDify})
				_, conversion := GetAdaptor(constant.APITypeDify).ConvertOpenAIRequest(c, info, request)
				allowed := sample.severity == "none" || policy == types.ConversionLossPolicyAllow || (sample.severity == "warning" && policy == types.ConversionLossPolicySafe)
				if allowed {
					require.NoError(t, planning)
					require.NoError(t, conversion)
				} else {
					require.Error(t, planning)
					require.Error(t, conversion)
				}
				if sample.severity != "none" {
					assert.NotEmpty(t, info.ConversionDiagnostics)
				}
				assert.False(t, info.UpstreamSent)
				assert.False(t, c.Writer.Written())
			})
		}
	}
}
