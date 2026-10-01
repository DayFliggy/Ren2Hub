package relayconvert

import (
	"context"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestToolMediaUpstreamHoistRetainsResultAndImage(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_output_tokens":4096,"input":[{"type":"function_call","call_id":"call_1","name":"screen","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"before"},{"type":"input_image","image_url":"https://example.test/p.png"},{"type":"input_text","text":"after"}]}]}`), &req))
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		t.Run(string(target), func(t *testing.T) {
			result, err := ConvertRequest(context.Background(), nil, target, &req)
			require.NoError(t, err)
			raw, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			assert.Contains(t, string(raw), "before")
			assert.Contains(t, string(raw), "after")
			assert.Contains(t, string(raw), "call_1")
			if target == types.RelayFormatOpenAI {
				chat := result.Value.(*dto.GeneralOpenAIRequest)
				var tool, user bool
				for _, m := range chat.Messages {
					if m.Role == "tool" {
						tool = true
						assert.NotContains(t, m.StringContent(), "p.png")
					}
					if m.Role == "user" {
						v, _ := kitutil.Marshal(m.Content)
						user = user || strings.Contains(string(v), "p.png")
					}
				}
				assert.True(t, tool)
				assert.True(t, user)
			}
		})
	}
	requirements, err := AnalyzeRequest(&req)
	require.NoError(t, err)
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow} {
		plan, err := PlanConversion(requirements, types.RelayFormatGemini, policy)
		require.Error(t, err)
		var loss *types.ConversionLossError
		require.ErrorAs(t, err, &loss)
		require.Len(t, plan.Diagnostics, 1)
		assert.Equal(t, "media_conversion_unavailable", plan.Diagnostics[0].Code)
	}
	_, err = ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &req)
	require.Error(t, err)
}
