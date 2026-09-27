package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizedUsageCalculatesProtocolCategoriesOnce(t *testing.T) {
	for _, test := range []struct {
		name, semantic, expression string
		usage                      dto.Usage
		context                    int64
		quota                      common.Quota
	}{
		{
			name: "OpenAI cached input belongs to total", semantic: "openai", expression: `p * 2.5 + c * 15 + cr * 0.25`,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200}}, context: 1000, quota: 4775,
		},
		{
			name: "Anthropic cached input is additive", semantic: "openai", expression: `p * 3 + c * 15 + cr * 0.3`,
			usage: dto.Usage{UsageSemantic: "anthropic", PromptTokens: 800, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200}}, context: 1000, quota: 4980,
		},
		{
			name: "Anthropic cache TTLs determine context tier", semantic: "anthropic", expression: `len == 9500 ? tier("full_context", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6) : 99999999`,
			usage: dto.Usage{PromptTokens: 5000, CompletionTokens: 2000, ClaudeCacheCreation5mTokens: 1000, ClaudeCacheCreation1hTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 3000, CachedCreationTokens: 1500}}, context: 9500, quota: 26325,
		},
		{
			name: "OpenAI audio image and reasoning are distinct", semantic: "openai", expression: `p * 2 + c * 10 + cr * 0.25 + img * 3 + ai * 5 + img_o * 6 + ao * 7 + reasoning * 8`,
			usage: dto.Usage{PromptTokens: 1000, CompletionTokens: 600, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 200, ImageTokens: 100, AudioTokens: 50}, CompletionTokenDetails: dto.OutputTokenDetails{ImageTokens: 30, AudioTokens: 100, ReasoningTokens: 20}}, context: 1000, quota: 3720,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			quote, err := billing.NewQuote(billing.PriceVersion{Model: "protocol-expression", Mode: billing.ModeExpression, Expression: test.expression}, "1", nil, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			usage := NormalizedBillingUsage(&test.usage, test.semantic)
			assert.Equal(t, test.context, usage.ContextTokens)
			result, err := billing.Calculate(quote, usage)
			require.NoError(t, err)
			assert.Equal(t, test.quota, result.FinalQuota)
		})
	}
}
