package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestChannelCompactTestRequiresExplicitEndpoint(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	for _, name := range []string{"gpt-5", "literal-openai-compact"} {
		assert.IsType(t, &dto.GeneralOpenAIRequest{}, buildTestRequest(name, "", channel, false))
		endpoint := string(constant.EndpointTypeOpenAIResponseCompact)
		request, ok := buildTestRequest(name, endpoint, channel, false).(*dto.OpenAIResponsesCompactionRequest)
		if assert.True(t, ok) {
			assert.Equal(t, name, request.Model)
		}
	}
}
