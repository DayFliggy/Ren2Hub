package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGeminiChatBillingUsageRequiresTokenContent(t *testing.T) {
	require.Nil(t, NewGeminiChatBillingUsage(nil))
	require.Nil(t, NewGeminiChatBillingUsage(&GeminiUsageMetadata{}))

	billingUsage := NewGeminiChatBillingUsage(&GeminiUsageMetadata{PromptTokenCount: 1})
	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.GeminiUsageMetadata)
	assert.Equal(t, BillingUsageSourceGeminiChat, billingUsage.Source)
	assert.Equal(t, BillingUsageSemanticGemini, billingUsage.Semantic)
	assert.False(t, billingUsage.Estimated)
}

func TestNewClaudeMessagesBillingUsageRequiresTokenContent(t *testing.T) {
	require.Nil(t, NewClaudeMessagesBillingUsage(nil))
	require.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{}))
	require.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{CacheCreation: &ClaudeCacheCreationUsage{}}))

	billingUsage := NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 1})
	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.ClaudeUsage)
	assert.Equal(t, BillingUsageSourceClaudeMessages, billingUsage.Source)
	assert.Equal(t, BillingUsageSemanticAnthropic, billingUsage.Semantic)

	cacheOnly := NewClaudeMessagesBillingUsage(&ClaudeUsage{
		CacheCreation: &ClaudeCacheCreationUsage{Ephemeral5mInputTokens: 4},
	})
	require.NotNil(t, cacheOnly)
}

func TestNewOpenAIChatBillingUsageRequiresTokenContent(t *testing.T) {
	require.Nil(t, NewOpenAIChatBillingUsage(nil))
	require.Nil(t, NewOpenAIChatBillingUsage(&Usage{}))

	billingUsage := NewOpenAIChatBillingUsage(&Usage{PromptTokens: 1})
	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.OpenAIUsage)
	assert.Equal(t, BillingUsageSourceOAIChat, billingUsage.Source)
	assert.Equal(t, BillingUsageSemanticOpenAI, billingUsage.Semantic)
	assert.Equal(t, 1, billingUsage.OpenAIUsage.PromptTokens)
}

func TestNewEstimatedGeminiChatBillingUsage(t *testing.T) {
	billingUsage := NewEstimatedGeminiChatBillingUsage(&Usage{
		PromptTokens:           11,
		CompletionTokens:       7,
		PromptTokensDetails:    InputTokenDetails{CachedTokens: 8, TextTokens: 6, ImageTokens: 3, AudioTokens: 2},
		CompletionTokenDetails: OutputTokenDetails{ReasoningTokens: 2, TextTokens: 3, AudioTokens: 2},
	})

	require.NotNil(t, billingUsage)
	require.NotNil(t, billingUsage.GeminiUsageMetadata)
	assert.True(t, billingUsage.Estimated)
	assert.Equal(t, 11, billingUsage.GeminiUsageMetadata.PromptTokenCount)
	assert.Equal(t, 5, billingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	assert.Equal(t, 2, billingUsage.GeminiUsageMetadata.ThoughtsTokenCount)
	assert.Equal(t, 18, billingUsage.GeminiUsageMetadata.TotalTokenCount)
	assert.Equal(t, 8, billingUsage.GeminiUsageMetadata.CachedContentTokenCount)
	assert.Equal(t, []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 6}, {Modality: "IMAGE", TokenCount: 3}, {Modality: "AUDIO", TokenCount: 2}}, billingUsage.GeminiUsageMetadata.PromptTokensDetails)
	assert.Equal(t, []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 3}, {Modality: "AUDIO", TokenCount: 2}}, billingUsage.GeminiUsageMetadata.CandidatesTokensDetails)
}

func TestCloneBillingUsageWithEstimatedCompletionPreservesProviderFacts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		source, want *BillingUsage
	}{
		{
			name: "openai responses",
			source: NewOpenAIResponsesBillingUsage(&Usage{InputTokens: 10, TotalTokens: 30,
				InputTokensDetails: &InputTokenDetails{CachedTokens: 8, AudioTokens: 2}}),
			want: &BillingUsage{Source: BillingUsageSourceOAIResponses, Semantic: BillingUsageSemanticOpenAI, Estimated: true,
				OpenAIUsage: &Usage{InputTokens: 10, CompletionTokens: 7, OutputTokens: 7, TotalTokens: 30,
					InputTokensDetails: &InputTokenDetails{CachedTokens: 8, AudioTokens: 2}}},
		},
		{
			name: "claude messages",
			source: NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 10, CacheReadInputTokens: 8,
				CacheCreation: &ClaudeCacheCreationUsage{Ephemeral1hInputTokens: 3}, ServerToolUse: &ClaudeServerToolUse{WebSearchRequests: 2}}),
			want: &BillingUsage{Source: BillingUsageSourceClaudeMessages, Semantic: BillingUsageSemanticAnthropic, Estimated: true,
				ClaudeUsage: &ClaudeUsage{InputTokens: 10, OutputTokens: 7, CacheReadInputTokens: 8,
					CacheCreation: &ClaudeCacheCreationUsage{Ephemeral1hInputTokens: 3}, ServerToolUse: &ClaudeServerToolUse{WebSearchRequests: 2}}},
		},
		{
			name: "gemini",
			source: NewGeminiChatBillingUsage(&GeminiUsageMetadata{PromptTokenCount: 10, ToolUsePromptTokenCount: 2,
				ThoughtsTokenCount: 3, TotalTokenCount: 15, CachedContentTokenCount: 8,
				PromptTokensDetails:        []GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 4}},
				ToolUsePromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 2}},
				CandidatesTokensDetails:    []GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 1}}}),
			want: &BillingUsage{Source: BillingUsageSourceGeminiChat, Semantic: BillingUsageSemanticGemini, Estimated: true,
				GeminiUsageMetadata: &GeminiUsageMetadata{PromptTokenCount: 10, ToolUsePromptTokenCount: 2,
					ThoughtsTokenCount: 3, CandidatesTokenCount: 4, TotalTokenCount: 19, CachedContentTokenCount: 8,
					PromptTokensDetails:        []GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 4}},
					ToolUsePromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 2}},
					CandidatesTokensDetails:    []GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 1}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, err := kitutil.Marshal(tc.source)
			require.NoError(t, err)
			patched := CloneBillingUsageWithEstimatedCompletion(tc.source, 7)
			require.NotNil(t, patched)
			assert.Equal(t, tc.want, patched)
			patched.Estimated = false
			assert.Equal(t, patched, CloneBillingUsageWithEstimatedCompletion(patched, 99), "known completion counts remain authoritative")
			assert.Equal(t, tc.source, CloneBillingUsageWithEstimatedCompletion(tc.source, 0))
			switch {
			case patched.OpenAIUsage != nil:
				patched.OpenAIUsage.InputTokensDetails.CachedTokens = 0
			case patched.ClaudeUsage != nil:
				patched.ClaudeUsage.CacheCreation.Ephemeral1hInputTokens = 0
				patched.ClaudeUsage.ServerToolUse.WebSearchRequests = 0
			case patched.GeminiUsageMetadata != nil:
				patched.GeminiUsageMetadata.PromptTokensDetails[0].TokenCount = 0
				patched.GeminiUsageMetadata.ToolUsePromptTokensDetails[0].TokenCount = 0
				patched.GeminiUsageMetadata.CandidatesTokensDetails[0].TokenCount = 0
			}
			after, err := kitutil.Marshal(tc.source)
			require.NoError(t, err)
			assert.JSONEq(t, string(original), string(after), "patching and subsequent mutations must not change the provider snapshot")
		})
	}
	assert.Nil(t, CloneBillingUsageWithEstimatedCompletion(nil, 7))
}

func TestBillingUsageJSONUsesProtocolNamedFields(t *testing.T) {
	billingUsage := &BillingUsage{
		OpenAIUsage:         &Usage{PromptTokens: 1, BillingUsage: NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 9})},
		ClaudeUsage:         &ClaudeUsage{InputTokens: 2, BillingUsage: NewOpenAIChatBillingUsage(&Usage{PromptTokens: 8})},
		GeminiUsageMetadata: &GeminiUsageMetadata{PromptTokenCount: 3, BillingUsage: NewOpenAIChatBillingUsage(&Usage{PromptTokens: 7})},
	}

	data, err := kitutil.Marshal(billingUsage)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"openai_usage"`)
	assert.Contains(t, string(data), `"claude_usage"`)
	assert.Contains(t, string(data), `"gemini_usage_metadata"`)
	assert.NotContains(t, string(data), `"usage":`)
	assert.NotContains(t, string(data), `"usage_metadata"`)

	clone := CloneBillingUsage(billingUsage)
	require.NotNil(t, clone.OpenAIUsage)
	require.NotNil(t, clone.ClaudeUsage)
	require.NotNil(t, clone.GeminiUsageMetadata)
	assert.Nil(t, clone.OpenAIUsage.BillingUsage)
	assert.Nil(t, clone.ClaudeUsage.BillingUsage)
	assert.Nil(t, clone.GeminiUsageMetadata.BillingUsage)
}
