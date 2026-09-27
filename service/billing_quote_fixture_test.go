package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func billingTestFinalUsage(t *testing.T, info *relaycommon.RelayInfo, input, output int64) {
	t.Helper()
	usage := &billing.Usage{InputTokens: input, OutputTokens: output, ContextTokens: input, Source: "provider", Complete: true, Terminal: "completed", Quantities: map[string]string{"calls": "1"}}
	charge, err := billing.Calculate(info.PriceData.Quote, *usage)
	require.NoError(t, err)
	info.PriceData.Usage, info.PriceData.Charge = usage, charge
}

func billingTestQuote(t *testing.T, expression string) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billingTestPublishedPrice(t, billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: expression}), "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}

func billingTestCallQuote(t *testing.T) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billingTestPublishedPrice(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.00004"}}}), "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}

func billingTestPublishedPrice(t *testing.T, version billing.PriceVersion) billing.PriceVersion {
	t.Helper()
	version.Provenance = &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit integration test price"}
	expected := int64(0)
	var active model.BillingActivePrice
	err := model.DB.Where("model = ? AND channel_id = ?", version.Model, version.ChannelID).First(&active).Error
	if err == nil {
		expected = active.VersionID
	} else {
		require.True(t, errors.Is(err, gorm.ErrRecordNotFound), "%v", err)
	}
	published, err := model.PublishBillingPrice(context.Background(), version, expected, 0)
	require.NoError(t, err)
	return *published
}
