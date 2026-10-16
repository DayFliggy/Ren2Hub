package service

import (
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestClaudePureCacheUsageRemainsBillable(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		read, write5, write1, quota int
	}{{"read", 20000, 0, 0, 2000}, {"5m write", 0, 20000, 0, 25000}, {"1h write", 0, 0, 20000, 40000}, {"zero", 0, 0, 0, 0}} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := quotedTextInfo(t, billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p*2+c*4+cr*0.2+cc*2.5+cc1h*4"})
			usage := &dto.Usage{BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{CacheReadInputTokens: tt.read, CacheCreationInputTokens: tt.write5 + tt.write1, ClaudeCacheCreation5mTokens: tt.write5, ClaudeCacheCreation1hTokens: tt.write1})}
			summary := calculateTextQuotaSummary(c, info, effectiveBillingUsage(usage))
			require.NoError(t, summary.PricingError)
			require.Equal(t, tt.quota, summary.Quota)
			require.Equal(t, tt.quota > 0, summary.hasBillableUsage())
			require.EqualValues(t, tt.read+tt.write5+tt.write1, info.PriceData.Usage.ContextTokens)
		})
	}
}
