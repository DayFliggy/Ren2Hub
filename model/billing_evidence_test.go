package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingEvidenceRejectsDifferentUsageWithIdenticalRoundedCharge(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "evidence-request")
	require.NoError(t, Reserve("evidence-request", 10))
	quote, err := publishedBillingTestQuote(billing.PriceVersion{Model: "test", ScopeKind: billing.PriceScopeChannel, ChannelID: 7, Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.0000001"}}}, "1", nil, time.Now())
	require.NoError(t, err)
	require.ErrorIs(t, PrepareBillingAttempt(context.Background(), "evidence-request", "attempt", 8, quote), ErrBillingEventConflict)
	require.NoError(t, PrepareBillingAttempt(context.Background(), "evidence-request", "attempt", 7, quote))
	usage := billing.Usage{Quantities: map[string]string{"calls": "1"}, Complete: true, Source: "provider", Terminal: "completed"}
	charge, err := billing.Calculate(quote, usage)
	require.NoError(t, err)
	consumption := BillingConsumption{Log: Log{UserId: 1, TokenId: 1, ChannelId: 7, ModelName: "test", Quota: charge.FinalQuota}, Usage: &usage, Charge: charge}
	require.NoError(t, PrepareBillingRecoveryConsumption("evidence-request", consumption))
	require.NoError(t, PrepareBillingRecoveryConsumption("evidence-request", consumption))
	usage.Quantities["calls"] = "2"
	consumption.Charge, err = billing.Calculate(quote, usage)
	require.NoError(t, err)
	assert.Equal(t, charge.FinalQuota, consumption.Charge.FinalQuota)
	require.ErrorIs(t, PrepareBillingRecoveryConsumption("evidence-request", consumption), ErrBillingRecoveryStateConflict)
	require.NoError(t, Settle("evidence-request", charge.FinalQuota))
	require.NoError(t, Settle("evidence-request", charge.FinalQuota))
	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "evidence-request").First(&recovery).Error)
	var event BillingEvent
	require.NoError(t, db.Where("event_id = ?", recovery.FinalEventID).First(&event).Error)
	assert.Len(t, recovery.ConsumptionFingerprint, 64)
	assert.Equal(t, recovery.ConsumptionFingerprint, event.EvidenceHash)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ? AND operation = ?", "evidence-request", "settle").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
