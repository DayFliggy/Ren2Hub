package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingEvidenceRejectsDifferentUsageWithIdenticalRoundedCharge(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "evidence-request")
	require.NoError(t, Reserve("evidence-request", 10))
	version, err := billing.NewModelPriceVersion("test", billing.PricingValues{"ModelPrice": "0.000002"}, false)
	require.NoError(t, err)
	quote, err := publishedBillingTestQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, PrepareBillingAttempt(context.Background(), "evidence-request", "attempt", 7, quote))
	require.ErrorIs(t, PrepareBillingAttempt(context.Background(), "evidence-request", "attempt", 8, quote), ErrBillingEventConflict)
	estimate, estimateCharge, err := EvaluateBilling(quote, billing.Usage{Estimated: true, Quantities: map[string]string{"calls": "1"}}, billingexpr.RequestInput{}, "evidence-request", "attempt", "estimate")
	require.NoError(t, err)
	require.NoError(t, PersistBillingEstimate("attempt", estimate, estimateCharge))
	usage, charge, err := EvaluateBilling(quote, billing.Usage{Quantities: map[string]string{"calls": "1"}, Complete: true, Source: "provider", Terminal: "completed"}, billingexpr.RequestInput{}, "evidence-request", "attempt", "settlement")
	require.NoError(t, err)
	consumption := BillingConsumption{AttemptID: "attempt", Log: Log{UserId: 1, TokenId: 1, ChannelId: 7, ModelName: "test", Quota: charge.FinalQuota}, Usage: &usage, Charge: charge}
	require.NoError(t, PrepareBillingRecoveryConsumption("evidence-request", consumption))
	require.NoError(t, PrepareBillingRecoveryConsumption("evidence-request", consumption))
	usage.Quantities["calls"] = "2"
	usage, consumption.Charge, err = EvaluateBilling(quote, usage, billingexpr.RequestInput{}, "evidence-request", "attempt", "settlement")
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
