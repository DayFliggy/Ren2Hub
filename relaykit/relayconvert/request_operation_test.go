package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyOpenAIRequestOperations(t *testing.T) {
	for _, test := range []struct {
		operation string
		request   *dto.GeneralOpenAIRequest
	}{
		{"completion", &dto.GeneralOpenAIRequest{Model: "test", Prompt: "continue this"}},
		{"moderation", &dto.GeneralOpenAIRequest{Model: "test", Input: []string{"inspect this"}}},
		{"edit", &dto.GeneralOpenAIRequest{Model: "test", Input: "old", Instruction: "correct this"}},
	} {
		t.Run(test.operation, func(t *testing.T) {
			req, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			assert.Equal(t, test.operation, req.Operation)
			for _, policy := range []types.ConversionLossPolicy{"safe", "strict", "allow"} {
				plan, err := PlanConversion(req, types.RelayFormatOpenAI, policy)
				require.NoError(t, err)
				assert.Equal(t, test.operation, plan.Operation)
				result, err := ExecuteConversionPlan(context.Background(), nil, plan, test.request)
				require.NoError(t, err)
				assert.Same(t, test.request, result.Value)
				for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini, types.RelayFormatOpenAIResponses} {
					_, err := PlanConversion(req, target, policy)
					require.ErrorContains(t, err, "not implemented")
					_, err = ConvertRequest(context.Background(), nil, target, test.request)
					require.ErrorContains(t, err, "not implemented")
				}
			}
		})
	}
}

func TestConversionPlanRejectsOperationMutation(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: "hello"}}}
	req, err := AnalyzeRequest(request)
	require.NoError(t, err)
	assert.Equal(t, "text", req.Operation)
	plan, err := PlanConversion(req, types.RelayFormatOpenAI, types.ConversionLossPolicySafe)
	require.NoError(t, err)
	request.Prompt = "changed operation"
	_, err = ExecuteConversionPlan(context.Background(), nil, plan, request)
	require.ErrorContains(t, err, "plan no longer matches")
}
