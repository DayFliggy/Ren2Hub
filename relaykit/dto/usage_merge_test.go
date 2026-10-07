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

func TestMergeGeminiUsageMetadataStreamDetailsAreCumulativeSnapshots(t *testing.T) {
	t.Parallel()

	prompt := []GeminiPromptTokensDetails{
		{Modality: "IMAGE", TokenCount: 1089},
		{Modality: "TEXT", TokenCount: 42},
	}
	frames := []*GeminiUsageMetadata{
		{PromptTokenCount: 1131, TotalTokenCount: 1131, PromptTokensDetails: prompt},
		{
			PromptTokenCount:           1131,
			ToolUsePromptTokenCount:    7,
			CandidatesTokenCount:       5,
			TotalTokenCount:            1143,
			PromptTokensDetails:        prompt,
			ToolUsePromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 7}},
			CandidatesTokensDetails:    []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 5}},
		},
		{
			PromptTokenCount:           1131,
			ToolUsePromptTokenCount:    7,
			CandidatesTokenCount:       1310,
			ThoughtsTokenCount:         76,
			TotalTokenCount:            2524,
			PromptTokensDetails:        prompt,
			ToolUsePromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 7}},
			CandidatesTokensDetails: []GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 20},
				{Modality: "IMAGE", TokenCount: 1290},
			},
		},
		// A trailing frame without details keeps the last reported ones.
		{PromptTokenCount: 1131, CandidatesTokenCount: 1310, ThoughtsTokenCount: 76, TotalTokenCount: 2524},
	}
	var merged *GeminiUsageMetadata
	for _, frame := range frames {
		merged = MergeGeminiUsageMetadataNonZero(merged, frame)
	}
	require.NotNil(t, merged)
	assert.Equal(t, prompt, merged.PromptTokensDetails)
	assert.Equal(t, []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 7}}, merged.ToolUsePromptTokensDetails)
	assert.Equal(t, []GeminiPromptTokensDetails{
		{Modality: "TEXT", TokenCount: 20},
		{Modality: "IMAGE", TokenCount: 1290},
	}, merged.CandidatesTokensDetails)

	streamUsage, ok := NewGeminiChatBillingUsage(merged).CanonicalUsage()
	require.True(t, ok)
	nonStreamUsage, ok := NewGeminiChatBillingUsage(frames[2]).CanonicalUsage()
	require.True(t, ok)
	assert.Equal(t, nonStreamUsage, streamUsage)
	assert.Equal(t, 1089, streamUsage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, 1290, streamUsage.CompletionTokenDetails.ImageTokens)

	// Converted streams remember the same cumulative sidecar at several
	// pipeline steps; repeated merges must not change the result.
	sidecar := NewGeminiChatBillingUsage(merged)
	again := MergeBillingUsageNonZero(MergeBillingUsageNonZero(sidecar, sidecar), sidecar)
	repeatedUsage, ok := again.CanonicalUsage()
	require.True(t, ok)
	assert.Equal(t, nonStreamUsage, repeatedUsage)
}

// Duplicate entries for one modality inside a snapshot settle as their sum;
// a later snapshot must replace every earlier entry for that modality rather
// than letting its duplicates overwrite one another.
func TestMergeGeminiUsageMetadataSnapshotWithDuplicateModalitySettlesAsSum(t *testing.T) {
	t.Parallel()

	snapshot := &GeminiUsageMetadata{
		PromptTokenCount: 25,
		PromptTokensDetails: []GeminiPromptTokensDetails{
			{Modality: "AUDIO", TokenCount: 10},
			{Modality: "audio", TokenCount: 15},
		},
	}
	earlier := &GeminiUsageMetadata{
		PromptTokenCount:    5,
		PromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 5}},
	}
	expected, ok := NewGeminiChatBillingUsage(snapshot).CanonicalUsage()
	require.True(t, ok)
	require.Equal(t, 25, expected.PromptTokensDetails.AudioTokens)

	for name, merged := range map[string]*GeminiUsageMetadata{
		"repeated snapshot":      MergeGeminiUsageMetadataNonZero(MergeGeminiUsageMetadataNonZero(nil, snapshot), snapshot),
		"after earlier snapshot": MergeGeminiUsageMetadataNonZero(earlier, snapshot),
	} {
		usage, ok := NewGeminiChatBillingUsage(merged).CanonicalUsage()
		require.True(t, ok, name)
		assert.Equal(t, 25, usage.PromptTokensDetails.AudioTokens, name)
	}
}
