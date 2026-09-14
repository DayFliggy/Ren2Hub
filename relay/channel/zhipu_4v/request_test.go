package zhipu_4v

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestZhipuV4PreservesChatEnvelope(t *testing.T) {
	body := `{"model":"glm-4v","messages":[{"role":"user","name":"client","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U=","detail":"low"},"vendor_hint":false}]},{"role":"assistant","reasoning_content":"analysis","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","name":"search","content":"result"}],"max_completion_tokens":0,"top_p":1,"temperature":0,"parallel_tool_calls":false,"stop":["END"],"metadata":{"case":"preserve"},"response_format":{"type":"json_schema","json_schema":{"name":"output","strict":true,"schema":{"type":"object"}}}}`
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(body), &request))
	before, err := common.Marshal(request)
	require.NoError(t, err)
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &request)
	require.NoError(t, err)
	actual, err := common.Marshal(converted)
	require.NoError(t, err)
	expected, err := sjson.Set(string(before), "messages.0.content.0.image_url.url", "aW1hZ2U=")
	require.NoError(t, err)
	expected, err = sjson.Delete(expected, "max_completion_tokens")
	require.NoError(t, err)
	expected, err = sjson.Set(expected, "max_tokens", 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, string(actual))
	original, err := common.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(original), "conversion must not mutate the retry source")
}
