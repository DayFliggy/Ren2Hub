package dto

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCacheCreationOnlyUsageSurvivesBillingCanonicalization(t *testing.T) {
	for _, usage := range []*Usage{
		{PromptTokensDetails: InputTokenDetails{CacheCreationInputTokens: 7}},
		{InputTokensDetails: &InputTokenDetails{CacheCreationInputTokens: 7}},
	} {
		for _, build := range []func(*Usage) *BillingUsage{NewOpenAIResponsesBillingUsage, NewOpenAIChatBillingUsage} {
			billing := build(usage)
			require.NotNil(t, billing)
			canonical, ok := billing.CanonicalUsage()
			require.True(t, ok)
			require.Equal(t, 7, canonical.PromptTokensDetails.CacheCreationTokensTotal())
			canonical.PromptTokensDetails.CacheCreationInputTokens = 99
			again, ok := billing.CanonicalUsage()
			require.True(t, ok)
			require.Equal(t, 7, again.PromptTokensDetails.CacheCreationTokensTotal())
		}
	}
	require.Nil(t, NewOpenAIResponsesBillingUsage(&Usage{}))
}
