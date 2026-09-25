package service

import (
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func billingTestQuote(t *testing.T, expression string) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: expression}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}

func billingTestCallQuote(t *testing.T) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.00004"}}}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}
