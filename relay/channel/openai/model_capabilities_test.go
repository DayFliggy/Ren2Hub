package openai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatCapabilitiesUseMappedModelAndEffort(t *testing.T) {
	for _, test := range []struct {
		model, effort                   string
		sampling, developer, completion bool
	}{
		{"gpt-5.2", "none", true, true, true},
		{"gpt-5.2-2025-12-11", "none", true, true, true},
		{"gpt-5.2", "high", false, true, true},
		{"gpt-5.2-codex", "none", false, true, true},
		{"gpt-6-astra", "none", false, true, true},
		{"gpt-future", "high", true, false, false},
	} {
		t.Run(test.model+"/"+test.effort, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			request := &dto.GeneralOpenAIRequest{Model: test.model, ReasoningEffort: test.effort, MaxTokens: common.GetPointer(uint(8)), Temperature: common.GetPointer(0.0), TopP: common.GetPointer(0.0), LogProbs: common.GetPointer(false), TopLogProbs: common.GetPointer(0), Messages: []dto.Message{{Role: "system", Content: "prompt"}}}
			info := &relaycommon.RelayInfo{OriginModelName: "client-alias", ReasoningEffort: test.effort, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: test.model}}
			converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, request)
			require.NoError(t, err)
			actual := converted.(*dto.GeneralOpenAIRequest)
			assert.Equal(t, test.sampling, actual.Temperature != nil)
			assert.Equal(t, test.sampling, actual.TopP != nil)
			assert.Equal(t, test.sampling, actual.LogProbs != nil)
			assert.Equal(t, test.sampling, actual.TopLogProbs != nil)
			assert.Equal(t, test.developer, actual.Messages[0].Role == "developer")
			assert.Equal(t, test.completion, actual.MaxCompletionTokens != nil)
			assert.Equal(t, !test.completion, actual.MaxTokens != nil)
			assert.Equal(t, test.effort, actual.ReasoningEffort)
		})
	}
}
