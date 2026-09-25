package dto

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGeminiCumulativeDetailsDoNotAccumulateTwice(t *testing.T) {
	initial := &GeminiUsageMetadata{PromptTokenCount: 10, PromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 3}}}
	result := MergeGeminiUsageMetadataNonZero(initial, initial)
	require.Len(t, result.PromptTokensDetails, 1)
	assert.Equal(t, 3, result.PromptTokensDetails[0].TokenCount)
}

func TestUsageMergePreservesExplicitZeroOutputModality(t *testing.T) {
	result := MergeUsageNonZero(&Usage{CompletionTokenDetails: OutputTokenDetails{ImageTokens: 5}}, &Usage{OutputTokensDetails: &OutputTokenDetails{}})
	require.NotNil(t, result.OutputTokensDetails)
	assert.Zero(t, result.CompletionTokenDetails.ImageTokens)
}
