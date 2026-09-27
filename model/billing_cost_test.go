package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupBillingCostTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&BillingUpstreamCost{}))
	seedBillingAccount(t, db, 1000, 1000, false)
	return db
}

func prepareBillingCostRequest(t *testing.T, requestID string, attemptIDs ...string) {
	t.Helper()
	newReservation(t, requestID)
	require.NoError(t, Reserve(requestID, 100))
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "cost-test-model", Mode: billing.ModeFree}, "1", nil, time.Now())
	require.NoError(t, err)
	for _, id := range attemptIDs {
		require.NoError(t, PrepareBillingAttempt(context.Background(), requestID, id, 7, quote))
	}
	require.NoError(t, UpdateBillingRecoverySettlement(requestID, -80))
	require.NoError(t, Settle(requestID, 20))
}

func TestBillingCostEvidenceIsImmutableIdempotentAndDoesNotChangeFunds(t *testing.T) {
	db := setupBillingCostTest(t)
	prepareBillingCostRequest(t, "cost-request", "cost-first", "cost-retry")
	ctx := context.Background()
	var userBefore User
	var tokenBefore Token
	var recoveryBefore BillingRecovery
	var eventsBefore []BillingEvent
	require.NoError(t, db.First(&userBefore, 1).Error)
	require.NoError(t, db.First(&tokenBefore, 1).Error)
	require.NoError(t, db.Where("request_id = ?", "cost-request").First(&recoveryBefore).Error)
	require.NoError(t, db.Order("id").Find(&eventsBefore).Error)

	summary, err := ReadBillingCostSummary(ctx, "cost-request")
	require.NoError(t, err)
	assert.Equal(t, 2, summary.AttemptCount)
	assert.False(t, summary.Complete)
	assert.Nil(t, summary.UpstreamCostUSD)
	command := BillingCostCommand{EventID: "invoice-first", AttemptID: "cost-first", AmountUSD: "0.0500", Source: BillingCostSourceInvoice, Reference: "Invoice 2026-09 line 5"}
	first, err := RecordBillingUpstreamCost(ctx, "cost-request", 3, command)
	require.NoError(t, err)
	assert.Equal(t, "0.05", first.AmountUSD)
	command.AmountUSD = "0.05"
	again, err := RecordBillingUpstreamCost(ctx, "cost-request", 3, command)
	require.NoError(t, err)
	assert.Equal(t, first, again)
	command.AmountUSD = "0.06"
	_, err = RecordBillingUpstreamCost(ctx, "cost-request", 3, command)
	require.ErrorIs(t, err, ErrBillingEventConflict)
	command.EventID = "invoice-first-again"
	_, err = RecordBillingUpstreamCost(ctx, "cost-request", 3, command)
	require.ErrorIs(t, err, ErrBillingEventConflict)
	assert.Error(t, db.Model(first).Update("amount_usd", "2").Error)
	assert.Error(t, db.Delete(first).Error)

	summary, err = ReadBillingCostSummary(ctx, "cost-request")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.ConfirmedAttemptCount)
	assert.False(t, summary.Complete)
	assert.Nil(t, summary.UpstreamCostUSD, "partial cost cannot be reported as the total")
	_, err = RecordBillingUpstreamCost(ctx, "cost-request", 3, BillingCostCommand{EventID: "receipt-retry", AttemptID: "cost-retry", AmountUSD: "0", Source: BillingCostSourceReceipt, Reference: "Receipt 728 retry not charged"})
	require.NoError(t, err)
	summary, err = ReadBillingCostSummary(ctx, "cost-request")
	require.NoError(t, err)
	require.True(t, summary.Complete)
	require.NotNil(t, summary.UpstreamCostUSD)
	assert.Equal(t, "0.05", *summary.UpstreamCostUSD)
	assert.Equal(t, 2, summary.ConfirmedAttemptCount)

	var userAfter User
	var tokenAfter Token
	var recoveryAfter BillingRecovery
	var eventsAfter []BillingEvent
	require.NoError(t, db.First(&userAfter, 1).Error)
	require.NoError(t, db.First(&tokenAfter, 1).Error)
	require.NoError(t, db.Where("request_id = ?", "cost-request").First(&recoveryAfter).Error)
	require.NoError(t, db.Order("id").Find(&eventsAfter).Error)
	assert.Equal(t, userBefore, userAfter)
	assert.Equal(t, tokenBefore, tokenAfter)
	assert.Equal(t, recoveryBefore, recoveryAfter)
	assert.Equal(t, eventsBefore, eventsAfter)
	assertBillingProjection(t, db, 1000, 1000)
}

func TestBillingCostRequiresFinalRequestAndMatchingAttempt(t *testing.T) {
	setupBillingCostTest(t)
	ctx := context.Background()
	prepareBillingCostRequest(t, "cost-one", "attempt-one")
	prepareBillingCostRequest(t, "cost-two", "attempt-two")
	command := BillingCostCommand{EventID: "receipt-one", AttemptID: "attempt-one", AmountUSD: "0", Source: BillingCostSourceReceipt, Reference: "Receipt 101"}
	_, err := RecordBillingUpstreamCost(ctx, "cost-two", 1, command)
	require.ErrorIs(t, err, ErrBillingEventConflict)
	_, err = RecordBillingUpstreamCost(ctx, "cost-one", 1, command)
	require.NoError(t, err)
	command.AttemptID = "attempt-two"
	_, err = RecordBillingUpstreamCost(ctx, "cost-two", 1, command)
	require.ErrorIs(t, err, ErrBillingEventConflict)

	newReservation(t, "pending-cost")
	command.EventID = "pending-receipt"
	_, err = RecordBillingUpstreamCost(ctx, "pending-cost", 1, command)
	require.ErrorIs(t, err, ErrBillingRecoveryStateConflict)
	zero, err := ReadBillingCostSummary(ctx, "cost-one")
	require.NoError(t, err)
	require.True(t, zero.Complete)
	require.NotNil(t, zero.UpstreamCostUSD)
	assert.Equal(t, "0", *zero.UpstreamCostUSD)
	unknown, err := ReadBillingCostSummary(ctx, "pending-cost")
	require.NoError(t, err)
	assert.False(t, unknown.Complete)
	assert.Nil(t, unknown.UpstreamCostUSD)
}

func TestBillingCostRejectsInvalidAmountAndRawReferences(t *testing.T) {
	setupBillingCostTest(t)
	prepareBillingCostRequest(t, "cost-validation", "validation-attempt")
	ctx := context.Background()
	base := BillingCostCommand{EventID: "invalid-receipt", AttemptID: "validation-attempt", AmountUSD: "0.1", Source: BillingCostSourceReceipt, Reference: "Receipt 500"}
	for _, amount := range []string{"", "-1", "NaN", "Inf", "1e19", "1e-19", strings.Repeat("9", 65), "1e2147483648"} {
		command := base
		command.AmountUSD = amount
		_, err := RecordBillingUpstreamCost(ctx, "cost-validation", 1, command)
		require.ErrorIs(t, err, ErrBillingCommandInvalid, amount)
	}
	for _, reference := range []string{"", strings.Repeat("a", 257), "receipt\nbody", "https://provider.invalid/receipt?token=example", "Authorization Bearer example", `{"raw":"receipt"}`} {
		command := base
		command.Reference = reference
		_, err := RecordBillingUpstreamCost(ctx, "cost-validation", 1, command)
		require.ErrorIs(t, err, ErrBillingCommandInvalid)
	}
	base.Source = "customer_price"
	_, err := RecordBillingUpstreamCost(ctx, "cost-validation", 1, base)
	require.ErrorIs(t, err, ErrBillingCommandInvalid)
	var count int64
	require.NoError(t, DB.Model(&BillingUpstreamCost{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestBillingCostConfirmedAmountRemainsDecimalString(t *testing.T) {
	setupBillingCostTest(t)
	prepareBillingCostRequest(t, "cost-precision", "precision-attempt")
	cost, err := RecordBillingUpstreamCost(context.Background(), "cost-precision", 1, BillingCostCommand{EventID: "precise-receipt", AttemptID: "precision-attempt",
		AmountUSD: "0.000000000000000001", Source: BillingCostSourceInvoice, Reference: "Invoice 601"})
	require.NoError(t, err)
	encoded, err := common.Marshal(cost)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"amount_usd":"0.000000000000000001"`)
	summary, err := ReadBillingCostSummary(context.Background(), "cost-precision")
	require.NoError(t, err)
	require.NotNil(t, summary.UpstreamCostUSD)
	assert.Equal(t, "0.000000000000000001", *summary.UpstreamCostUSD)
}

func TestBillingCostRefundedAttemptNeedsExplicitEvidence(t *testing.T) {
	setupBillingCostTest(t)
	ctx := context.Background()
	newReservation(t, "refunded-cost")
	require.NoError(t, Reserve("refunded-cost", 10))
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "cost-test-model", Mode: billing.ModeFree}, "1", nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, PrepareBillingAttempt(ctx, "refunded-cost", "refunded-attempt", 7, quote))
	require.NoError(t, MarkBillingRecoveryRefundPending("refunded-cost", nil))
	require.NoError(t, Release("refunded-cost"))
	unknown, err := ReadBillingCostSummary(ctx, "refunded-cost")
	require.NoError(t, err)
	assert.False(t, unknown.Complete)
	assert.Nil(t, unknown.UpstreamCostUSD)
	_, err = RecordBillingUpstreamCost(ctx, "refunded-cost", 1, BillingCostCommand{EventID: "refund-receipt", AttemptID: "refunded-attempt", AmountUSD: "0", Source: BillingCostSourceReceipt, Reference: "Refund receipt 802"})
	require.NoError(t, err)
	confirmed, err := ReadBillingCostSummary(ctx, "refunded-cost")
	require.NoError(t, err)
	require.True(t, confirmed.Complete)
	require.NotNil(t, confirmed.UpstreamCostUSD)
	assert.Equal(t, "0", *confirmed.UpstreamCostUSD)
}

func TestBillingCostSummaryRejectsChangedEvidence(t *testing.T) {
	db := setupBillingCostTest(t)
	prepareBillingCostRequest(t, "changed-cost", "changed-attempt")
	_, err := RecordBillingUpstreamCost(context.Background(), "changed-cost", 1, BillingCostCommand{EventID: "changed-receipt", AttemptID: "changed-attempt", AmountUSD: "0.1", Source: BillingCostSourceInvoice, Reference: "Invoice 911"})
	require.NoError(t, err)
	// Bypass the immutable model hook to simulate damaged persisted evidence.
	require.NoError(t, db.Exec("UPDATE billing_upstream_costs SET amount_usd = ? WHERE attempt_id = ?", "0.2", "changed-attempt").Error)
	summary, err := ReadBillingCostSummary(context.Background(), "changed-cost")
	require.ErrorContains(t, err, "evidence integrity mismatch")
	assert.Nil(t, summary.UpstreamCostUSD)
	assert.False(t, summary.Complete)
}
