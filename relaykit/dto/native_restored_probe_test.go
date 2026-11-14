//go:build known_upstream_restored

package dto

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRestoredProbeOpenAICacheCreationInputTokensAloneIsBillableUsage(t *testing.T) {
	for _, usage := range []*Usage{
		{PromptTokensDetails: InputTokenDetails{CacheCreationInputTokens: 7}},
		{InputTokensDetails: &InputTokenDetails{CacheCreationInputTokens: 7}},
	} {
		billing := NewOpenAIResponsesBillingUsage(usage)
		require.NotNil(t, billing)
		canonical, ok := billing.CanonicalUsage()
		require.True(t, ok)
		assert.Equal(t, 7, canonical.PromptTokensDetails.CacheCreationTokensTotal())
	}
}
