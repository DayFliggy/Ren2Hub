package model

import (
	"crypto/sha256"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/shopspring/decimal"
)

const (
	BillingAuditUnconfirmed     = "unconfirmed"
	BillingAuditVerified        = "verified"
	BillingAuditMismatch        = "mismatch"
	BillingAuditInvalidEvidence = "invalid_evidence"
)

type BillingAuditAttempt struct {
	ID                 string `json:"id"`
	ChannelID          int    `json:"channel_id"`
	RequestFingerprint string `json:"request_fingerprint"`
	PriceScope         string `json:"price_scope"`
	PriceVersionID     int64  `json:"price_version_id"`
	PriceVersionHash   string `json:"price_version_hash"`
	QuoteHash          string `json:"quote_hash"`
	Origin             string `json:"origin"`
	SourceHash         string `json:"source_hash"`
}

// BillingAudit reports evidence consistency, never a replacement accounting
// decision. Customer money comes only from committed settlement events.
type BillingAudit struct {
	Status                  string                `json:"status"`
	Reason                  string                `json:"reason,omitempty"`
	Attempts                []BillingAuditAttempt `json:"attempts"`
	QuoteSnapshotHash       string                `json:"quote_snapshot_hash,omitempty"`
	UsageSnapshotHash       string                `json:"usage_snapshot_hash,omitempty"`
	ChargeSnapshotHash      string                `json:"charge_snapshot_hash,omitempty"`
	ReplayedCharge          *billing.ChargeResult `json:"replayed_charge"`
	QuotedTotalUSD          *string               `json:"quoted_total_usd"`
	CustomerChargeUSD       string                `json:"customer_charge_usd"`
	CustomerChargeQuota     common.Quota          `json:"customer_charge_quota"`
	CustomerChargeConfirmed bool                  `json:"customer_charge_confirmed"`
	ChargeDifferenceUSD     *string               `json:"charge_difference_usd"`
	ChargeDifferenceQuota   *common.Quota         `json:"charge_difference_quota"`
	UpstreamCostUSD         *string               `json:"upstream_cost_usd"`
	ProfitUSD               *string               `json:"profit_usd"`
	CostStatus              string                `json:"cost_status"`
}

// AuditBillingEvidence is deliberately independent of the database and current
// price catalog. Callers supply one saved request's immutable evidence.
func AuditBillingEvidence(recovery BillingRecovery, attempts []BillingAttempt, events []BillingEvent) BillingAudit {
	audit := BillingAudit{Status: BillingAuditUnconfirmed, Reason: "confirmed_evidence_missing", Attempts: []BillingAuditAttempt{}, CustomerChargeUSD: "0", CostStatus: "unknown"}
	for _, snapshot := range []struct {
		raw  string
		hash *string
	}{
		{recovery.QuoteSnapshot, &audit.QuoteSnapshotHash},
		{recovery.UsageSnapshot, &audit.UsageSnapshotHash},
		{recovery.ChargeSnapshot, &audit.ChargeSnapshotHash},
	} {
		if snapshot.raw != "" {
			*snapshot.hash = fmt.Sprintf("%x", sha256.Sum256([]byte(snapshot.raw)))
		}
	}
	var final *BillingEvent
	for i := range events {
		event := &events[i]
		if event.RequestID != recovery.RequestID || event.UserID != recovery.UserID ||
			(event.TokenID != 0 || recovery.TokenRequired) && event.TokenID != recovery.TokenID {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "event_identity_mismatch"
			return audit
		}
		if event.Operation != "settle" && event.Operation != "release" {
			continue
		}
		if final != nil || event.UsedDelta < 0 || event.EventID != recovery.FinalEventID {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "final_event_mismatch"
			return audit
		}
		final = event
		if event.Operation == "settle" {
			audit.CustomerChargeQuota = event.UsedDelta
			audit.CustomerChargeUSD = decimal.NewFromInt(int64(event.UsedDelta)).Div(decimal.NewFromInt(billing.QuotaPerUSD)).String()
		}
	}
	if final != nil {
		if final.Operation == "settle" && recovery.Status != BillingRecoveryStatusSettled ||
			final.Operation == "release" && (recovery.Status != BillingRecoveryStatusRefunded || final.UsedDelta != 0) {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "final_event_state_mismatch"
			return audit
		}
		audit.CustomerChargeConfirmed = true
	}
	var selected *billing.Quote
	if recovery.QuoteSnapshot != "" {
		if err := common.UnmarshalJsonStr(recovery.QuoteSnapshot, &selected); err != nil || selected == nil {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "quote_snapshot_invalid"
			return audit
		}
	}
	matched := false
	for _, attempt := range attempts {
		var quote billing.Quote
		if attempt.RequestID != recovery.RequestID || common.UnmarshalJsonStr(attempt.Quote, &quote) != nil || attempt.QuoteHash != quote.Hash || validateBillingAuditQuote(quote) != nil {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "attempt_quote_invalid"
			return audit
		}
		if quote.PriceVersion.ScopeKind == billing.PriceScopeChannel && quote.PriceVersion.ChannelID != attempt.ChannelID {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "attempt_scope_mismatch"
			return audit
		}
		fingerprint, err := BillingAttemptFingerprint(attempt.RequestID, attempt.ChannelID, &quote)
		if err != nil || fingerprint != attempt.Fingerprint {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "attempt_fingerprint_invalid"
			return audit
		}
		audit.Attempts = append(audit.Attempts, BillingAuditAttempt{ID: attempt.ID, ChannelID: attempt.ChannelID, RequestFingerprint: attempt.Fingerprint, PriceScope: quote.PriceVersion.ScopeKind, PriceVersionID: quote.PriceVersion.ID, PriceVersionHash: quote.PriceVersion.Hash, QuoteHash: quote.Hash, Origin: quote.PriceVersion.Origin, SourceHash: quote.PriceVersion.SourceHash})
		if selected != nil && quote.Hash == selected.Hash {
			matched = true
		}
	}
	if selected == nil {
		return audit
	}
	if validateBillingAuditQuote(*selected) != nil || !matched {
		audit.Status, audit.Reason = BillingAuditInvalidEvidence, "selected_quote_invalid"
		return audit
	}
	var usage *billing.Usage
	var saved *billing.ChargeResult
	if recovery.UsageSnapshot == "" || recovery.ChargeSnapshot == "" {
		return audit
	}
	if common.UnmarshalJsonStr(recovery.UsageSnapshot, &usage) != nil || common.UnmarshalJsonStr(recovery.ChargeSnapshot, &saved) != nil {
		audit.Status, audit.Reason = BillingAuditInvalidEvidence, "calculation_snapshot_invalid"
		return audit
	}
	if usage == nil || saved == nil {
		return audit
	}
	if recovery.ConsumptionFingerprint == "" {
		return audit
	}
	fingerprint, err := BillingEvidenceFingerprint(recovery.RequestID, selected.Hash, usage, saved)
	if err != nil || fingerprint != recovery.ConsumptionFingerprint {
		audit.Status, audit.Reason = BillingAuditInvalidEvidence, "consumption_fingerprint_invalid"
		return audit
	}
	if final != nil && final.Operation == "settle" && final.EvidenceHash != fingerprint {
		audit.Status, audit.Reason = BillingAuditInvalidEvidence, "settlement_evidence_mismatch"
		return audit
	}
	replayed, err := billing.Calculate(selected, *usage)
	if err != nil {
		audit.Status, audit.Reason = BillingAuditInvalidEvidence, "frozen_calculation_invalid"
		return audit
	}
	audit.ReplayedCharge, audit.QuotedTotalUSD = replayed, &replayed.TotalUSD
	if final != nil && final.Operation == "settle" {
		difference, err := common.SubtractQuota(audit.CustomerChargeQuota, replayed.FinalQuota)
		if err != nil {
			audit.Status, audit.Reason = BillingAuditInvalidEvidence, "charge_difference_overflow"
			return audit
		}
		differenceUSD := decimal.NewFromInt(int64(difference)).Div(decimal.NewFromInt(billing.QuotaPerUSD)).String()
		audit.ChargeDifferenceQuota, audit.ChargeDifferenceUSD = &difference, &differenceUSD
	}
	savedHash, savedErr := billing.Hash(saved)
	replayedHash, replayedErr := billing.Hash(replayed)
	if savedErr != nil || replayedErr != nil || savedHash != replayedHash {
		audit.Status, audit.Reason = BillingAuditMismatch, "saved_charge_differs_from_replay"
		return audit
	}
	if !usage.Complete || usage.Estimated || BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) || BillingRecoveryUsageCollecting(recovery.ConsumptionState) {
		audit.Reason = "usage_unconfirmed"
		return audit
	}
	if final == nil || final.Operation != "settle" {
		return audit
	}
	if recovery.Status != BillingRecoveryStatusSettled || *audit.ChargeDifferenceQuota != 0 {
		audit.Status, audit.Reason = BillingAuditMismatch, "settlement_differs_from_replay"
		return audit
	}
	audit.Status, audit.Reason = BillingAuditVerified, ""
	return audit
}

func validateBillingAuditQuote(quote billing.Quote) error {
	version := quote.PriceVersion
	if version.ID <= 0 || version.Hash == "" {
		return fmt.Errorf("published price identity missing")
	}
	version.ID, version.CreatedAt, version.Hash = 0, 0, ""
	hash, err := billing.Hash(version)
	if err != nil || hash != quote.PriceVersion.Hash {
		return fmt.Errorf("price version hash mismatch")
	}
	if err := billing.ValidateFrozenPriceVersion(quote.PriceVersion); err != nil {
		return err
	}
	quoteHash := quote.Hash
	quote.Hash = ""
	hash, err = billing.Hash(quote)
	if err != nil || hash != quoteHash {
		return fmt.Errorf("quote hash mismatch")
	}
	return nil
}

// Cost evidence is independent from selling price. Profit stays unknown until
// both the customer settlement and every upstream attempt are confirmed.
func (audit *BillingAudit) ApplyUpstreamCost(costUSD *string, complete bool) error {
	if !complete {
		return nil
	}
	if costUSD == nil {
		return fmt.Errorf("confirmed upstream cost is missing")
	}
	cost, err := parseBillingCostAmount(*costUSD)
	if err != nil {
		return err
	}
	normalized := cost.String()
	audit.UpstreamCostUSD, audit.CostStatus = &normalized, "confirmed"
	if !audit.CustomerChargeConfirmed {
		return nil
	}
	customer, err := decimal.NewFromString(audit.CustomerChargeUSD)
	if err != nil {
		return err
	}
	profit := customer.Sub(cost).String()
	audit.ProfitUSD = &profit
	return nil
}
