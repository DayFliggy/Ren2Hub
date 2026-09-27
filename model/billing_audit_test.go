package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settledBillingAuditEvidence(t *testing.T) (BillingRecovery, []BillingAttempt, []BillingEvent) {
	t.Helper()
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&BillingPriceVersion{}, &BillingActivePrice{}))
	seedBillingAccount(t, db, 1000, 1000, false)
	version, err := PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		Model: "audit-model", ScopeKind: billing.PriceScopeChannel, ChannelID: 7,
		Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}},
	}, 0, 1)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*version, "1", nil, time.Now())
	require.NoError(t, err)
	usage := &billing.Usage{InputTokens: 100, Complete: true, Source: "provider"}
	charge, err := billing.Calculate(quote, *usage)
	require.NoError(t, err)
	newReservation(t, "audit-request")
	require.NoError(t, Reserve("audit-request", 100))
	require.NoError(t, PrepareBillingAttempt(context.Background(), "audit-request", "audit-attempt", 7, quote))
	require.NoError(t, PrepareBillingRecoveryConsumption("audit-request", BillingConsumption{
		Usage: usage, Charge: charge,
		Log:        Log{UserId: 1, TokenId: 1, ChannelId: 7, ModelName: "audit-model", Quota: charge.FinalQuota, Type: LogTypeConsume},
		CountUsage: true,
	}))
	require.NoError(t, Settle("audit-request", charge.FinalQuota))
	var recovery BillingRecovery
	var attempts []BillingAttempt
	var events []BillingEvent
	require.NoError(t, db.Where("request_id = ?", "audit-request").First(&recovery).Error)
	require.NoError(t, db.Where("request_id = ?", "audit-request").Find(&attempts).Error)
	require.NoError(t, db.Where("request_id = ?", "audit-request").Order("id").Find(&events).Error)
	return recovery, attempts, events
}

func TestBillingAuditReplaysFrozenPricesAndSeparatesActualCost(t *testing.T) {
	recovery, attempts, events := settledBillingAuditEvidence(t)
	var quote billing.Quote
	require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
	changed := quote.PriceVersion
	changed.Prices[0].Price = "1000"
	_, err := PublishBillingPrice(context.Background(), changed, quote.PriceVersion.ID, 1)
	require.NoError(t, err)

	audit := AuditBillingEvidence(recovery, attempts, events)
	require.Equal(t, BillingAuditVerified, audit.Status, audit.Reason)
	require.NotNil(t, audit.QuotedTotalUSD)
	assert.Equal(t, "0.0002", *audit.QuotedTotalUSD)
	assert.Equal(t, "0.0002", audit.CustomerChargeUSD)
	assert.True(t, audit.CustomerChargeConfirmed)
	assert.EqualValues(t, 100, audit.CustomerChargeQuota)
	require.NotNil(t, audit.ChargeDifferenceQuota)
	assert.Zero(t, *audit.ChargeDifferenceQuota)
	assert.Equal(t, quote.PriceVersion.ID, audit.Attempts[0].PriceVersionID)
	assert.Equal(t, BillingPriceOriginManual, audit.Attempts[0].Origin)
	assert.NotEmpty(t, audit.Attempts[0].RequestFingerprint)
	assert.NotEmpty(t, audit.UsageSnapshotHash)
	assert.Nil(t, audit.UpstreamCostUSD)
	assert.Nil(t, audit.ProfitUSD)
	cost := "0.00005"
	require.NoError(t, audit.ApplyUpstreamCost(&cost, false))
	assert.Nil(t, audit.UpstreamCostUSD)
	require.NoError(t, audit.ApplyUpstreamCost(&cost, true))
	require.NotNil(t, audit.ProfitUSD)
	assert.Equal(t, "0.00015", *audit.ProfitUSD)
	assert.Equal(t, "confirmed", audit.CostStatus)
}

func TestBillingAuditRejectsTamperedEvidence(t *testing.T) {
	recovery, attempts, events := settledBillingAuditEvidence(t)
	for _, test := range []struct {
		name   string
		mutate func(*BillingRecovery, []BillingAttempt, []BillingEvent)
	}{
		{"quote", func(r *BillingRecovery, _ []BillingAttempt, _ []BillingEvent) {
			var q billing.Quote
			require.NoError(t, common.UnmarshalJsonStr(r.QuoteSnapshot, &q))
			q.ChannelRatio = "2"
			raw, err := common.Marshal(q)
			require.NoError(t, err)
			r.QuoteSnapshot = string(raw)
		}},
		{"usage_unpriced_field", func(r *BillingRecovery, _ []BillingAttempt, _ []BillingEvent) {
			var usage billing.Usage
			require.NoError(t, common.UnmarshalJsonStr(r.UsageSnapshot, &usage))
			usage.Source = "altered-source"
			raw, err := common.Marshal(usage)
			require.NoError(t, err)
			r.UsageSnapshot = string(raw)
		}},
		{"charge", func(r *BillingRecovery, _ []BillingAttempt, _ []BillingEvent) {
			var charge billing.ChargeResult
			require.NoError(t, common.UnmarshalJsonStr(r.ChargeSnapshot, &charge))
			charge.Lines[0].UnitPrice = "9"
			raw, err := common.Marshal(charge)
			require.NoError(t, err)
			r.ChargeSnapshot = string(raw)
		}},
		{"attempt_fingerprint", func(_ *BillingRecovery, a []BillingAttempt, _ []BillingEvent) { a[0].Fingerprint = "altered" }},
		{"attempt_scope", func(_ *BillingRecovery, a []BillingAttempt, _ []BillingEvent) { a[0].ChannelID = 8 }},
		{"event_identity", func(_ *BillingRecovery, _ []BillingAttempt, e []BillingEvent) { e[0].RequestID = "different-request" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, a, e := recovery, append([]BillingAttempt(nil), attempts...), append([]BillingEvent(nil), events...)
			test.mutate(&r, a, e)
			audit := AuditBillingEvidence(r, a, e)
			assert.Equal(t, BillingAuditInvalidEvidence, audit.Status)
			assert.Nil(t, audit.ProfitUSD)
		})
	}
}

func TestBillingAuditRequiresCompleteUsageAndConfirmedSettlement(t *testing.T) {
	recovery, attempts, events := settledBillingAuditEvidence(t)
	var quote billing.Quote
	var usage billing.Usage
	var charge billing.ChargeResult
	require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
	require.NoError(t, common.UnmarshalJsonStr(recovery.UsageSnapshot, &usage))
	require.NoError(t, common.UnmarshalJsonStr(recovery.ChargeSnapshot, &charge))
	t.Run("partial_usage", func(t *testing.T) {
		r, partial := recovery, usage
		partial.Complete = false
		raw, err := common.Marshal(partial)
		require.NoError(t, err)
		r.UsageSnapshot = string(raw)
		r.ConsumptionFingerprint, err = BillingEvidenceFingerprint(r.RequestID, quote.Hash, &partial, &charge)
		require.NoError(t, err)
		e := append([]BillingEvent(nil), events...)
		for i := range e {
			if e[i].Operation == "settle" {
				e[i].EvidenceHash = r.ConsumptionFingerprint
			}
		}
		audit := AuditBillingEvidence(r, attempts, e)
		assert.Equal(t, BillingAuditUnconfirmed, audit.Status)
		assert.True(t, audit.CustomerChargeConfirmed, "usage confirmation is independent from the committed customer charge")
		cost := "0"
		require.NoError(t, audit.ApplyUpstreamCost(&cost, true))
		require.NotNil(t, audit.ProfitUSD)
		assert.Equal(t, audit.CustomerChargeUSD, *audit.ProfitUSD)
	})
	t.Run("refund", func(t *testing.T) {
		r := recovery
		r.Status = BillingRecoveryStatusRefunded
		e := append([]BillingEvent(nil), events...)
		for i := range e {
			if e[i].Operation == "settle" {
				e[i].Operation, e[i].UsedDelta = "release", 0
			}
		}
		audit := AuditBillingEvidence(r, attempts, e)
		assert.Equal(t, BillingAuditUnconfirmed, audit.Status)
		assert.Equal(t, "0", audit.CustomerChargeUSD)
		assert.True(t, audit.CustomerChargeConfirmed)
		cost := "0.0001"
		require.NoError(t, audit.ApplyUpstreamCost(&cost, true))
		require.NotNil(t, audit.ProfitUSD)
		assert.Equal(t, "-0.0001", *audit.ProfitUSD)
	})
	t.Run("different_committed_amount", func(t *testing.T) {
		e := append([]BillingEvent(nil), events...)
		for i := range e {
			if e[i].Operation == "settle" {
				e[i].UsedDelta += 5
			}
		}
		audit := AuditBillingEvidence(recovery, attempts, e)
		assert.Equal(t, BillingAuditMismatch, audit.Status)
		assert.Equal(t, "0.00021", audit.CustomerChargeUSD)
		require.NotNil(t, audit.ChargeDifferenceQuota)
		assert.EqualValues(t, 5, *audit.ChargeDifferenceQuota)
		assert.Equal(t, "0.00001", *audit.ChargeDifferenceUSD)
		assert.True(t, audit.CustomerChargeConfirmed)
		cost := "0.0001"
		require.NoError(t, audit.ApplyUpstreamCost(&cost, true))
		require.NotNil(t, audit.ProfitUSD)
		assert.Equal(t, "0.00011", *audit.ProfitUSD)
	})
}

func TestBillingAuditNeverConfirmsMissingOrInvalidFinalEvents(t *testing.T) {
	recovery, attempts, events := settledBillingAuditEvidence(t)
	for _, test := range []struct {
		name   string
		mutate func(*BillingRecovery, *[]BillingEvent)
	}{
		{"missing", func(_ *BillingRecovery, e *[]BillingEvent) { *e = (*e)[:1] }},
		{"wrong_state", func(r *BillingRecovery, _ *[]BillingEvent) { r.Status = BillingRecoveryStatusActive }},
		{"wrong_request", func(_ *BillingRecovery, e *[]BillingEvent) { (*e)[len(*e)-1].RequestID = "other-request" }},
		{"missing_token", func(_ *BillingRecovery, e *[]BillingEvent) { (*e)[len(*e)-1].TokenID = 0 }},
		{"refund_with_charge", func(r *BillingRecovery, e *[]BillingEvent) {
			r.Status = BillingRecoveryStatusRefunded
			(*e)[len(*e)-1].Operation = "release"
		}},
		{"duplicate_final", func(_ *BillingRecovery, e *[]BillingEvent) { *e = append(*e, (*e)[len(*e)-1]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, e := recovery, append([]BillingEvent(nil), events...)
			test.mutate(&r, &e)
			audit := AuditBillingEvidence(r, attempts, e)
			assert.False(t, audit.CustomerChargeConfirmed)
			cost := "0.0001"
			require.NoError(t, audit.ApplyUpstreamCost(&cost, true))
			assert.Nil(t, audit.ProfitUSD)
		})
	}
}
