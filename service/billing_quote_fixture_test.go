package service

import (
	"context"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func billingTestFinalUsage(t *testing.T, info *relaycommon.RelayInfo, input, output int64) {
	t.Helper()
	usage := billing.Usage{InputTokens: input, OutputTokens: output, ContextTokens: input, InputIncludesCache: true, Source: "provider", Complete: true, Terminal: "completed", Quantities: map[string]string{"calls": "1"}}
	require.NotEmpty(t, info.BillingAttemptID)
	usage, charge, err := model.EvaluateBilling(info.PriceData.Quote, usage, billingexpr.RequestInput{}, info.RequestId, info.BillingAttemptID, "settlement")
	require.NoError(t, err)
	info.PriceData.Usage, info.PriceData.Charge = &usage, charge
}

func billingTestEstimate(t *testing.T, info *relaycommon.RelayInfo) {
	t.Helper()
	info.BillingAttemptID = info.RequestId + ":attempt"
	if info.BillingRequestInput == nil {
		info.BillingRequestInput = &billingexpr.RequestInput{}
	}
	usage := billing.Usage{InputTokens: int64(info.PriceData.QuotaToPreConsume), ContextTokens: int64(info.PriceData.QuotaToPreConsume), Estimated: true, Source: "request_estimate", InputIncludesCache: true}
	usage, charge, err := model.EvaluateBilling(info.PriceData.Quote, usage, *info.BillingRequestInput, info.RequestId, info.BillingAttemptID, "estimate")
	require.NoError(t, err)
	info.PriceData.Usage, info.PriceData.Charge = &usage, charge
}

func billingTestQuote(t *testing.T, expression string) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billingTestStoredPrice(t, "test", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}), "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}

func billingTestCallQuote(t *testing.T) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billingTestStoredPrice(t, "test", billing.PricingValues{"ModelPrice": "0.00004"}), "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}

func billingTestStoredPrice(t *testing.T, name string, values billing.PricingValues) billing.PriceVersion {
	t.Helper()
	version, err := billing.NewModelPriceVersion(name, values, false)
	require.NoError(t, err)
	published, err := model.StoreBillingPriceEvidence(context.Background(), version)
	require.NoError(t, err)
	return *published
}
