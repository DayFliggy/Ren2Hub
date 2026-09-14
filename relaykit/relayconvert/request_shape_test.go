package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestShapeKeepsPresenceWithoutRequestContent(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"temperature":0,"parallel_tool_calls":false,"messages":[{"role":"user","content":[{"type":"text","text":"private prompt"},{"type":"image_url","image_url":{"url":"https://private.invalid/secret"}}]}]}`), &request))
	requirements, err := AnalyzeRequest(&request)
	require.NoError(t, err)
	require.NotNil(t, requirements.Shape)
	assert.Contains(t, requirements.Shape.Fields, "temperature")
	assert.Contains(t, requirements.Shape.Fields, "parallel_tool_calls")
	assert.NotContains(t, requirements.Shape.Fields, "top_p")
	require.Len(t, requirements.Shape.Messages, 1)
	assert.Equal(t, []ContentPartShape{{Type: "text", Fields: []string{"text", "type"}}, {Type: "image_url", Fields: []string{"image_url", "type", "image_url.url"}}}, requirements.Shape.Messages[0].Content)
	encoded, err := kitutil.Marshal(requirements)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private prompt")
	assert.NotContains(t, string(encoded), "private.invalid")
	request.Messages[0].Content = 42
	_, err = AnalyzeRequest(&request)
	require.ErrorContains(t, err, "content must be a string, array or null")
}
