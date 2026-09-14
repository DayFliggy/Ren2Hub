package relayconvert

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultipleCandidatesRequireRepresentableTarget(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		request any
	}{
		{"chat", `{"model":"test","n":2,"messages":[{"role":"user","content":"hello"}]}`, &dto.GeneralOpenAIRequest{}},
		{"gemini", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"candidateCount":2}}`, &dto.GeminiChatRequest{}},
		{"gemini_alias", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"candidate_count":2}}`, &dto.GeminiChatRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, json.Unmarshal([]byte(tc.body), tc.request))
			requirements, err := AnalyzeRequest(tc.request)
			require.NoError(t, err)
			require.Contains(t, requirements.Features, "multiple_candidates")
			for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
					plan, err := PlanConversion(requirements, target, policy)
					require.Error(t, err)
					require.Len(t, plan.Diagnostics, 1)
					assert.Equal(t, "multiple_candidates_unsupported", plan.Diagnostics[0].Code)
				}
			}
			_, err = PlanConversion(requirements, requirements.Format, types.ConversionLossPolicySafe)
			require.NoError(t, err, "native Chat and Gemini keep their candidate support")
		})
	}
}
