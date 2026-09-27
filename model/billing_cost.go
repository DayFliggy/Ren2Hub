package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingCostSourceInvoice = "invoice"
	BillingCostSourceReceipt = "provider_receipt"
)

// BillingUpstreamCost records confirmed provider evidence. It never changes
// the customer's charge and cannot be inferred from a customer price version.
type BillingUpstreamCost struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	EventID     string `json:"event_id" gorm:"type:varchar(192);not null;uniqueIndex"`
	RequestID   string `json:"request_id" gorm:"type:varchar(128);not null;index"`
	AttemptID   string `json:"attempt_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	AmountUSD   string `json:"amount_usd" gorm:"type:varchar(64);not null"`
	Source      string `json:"source" gorm:"type:varchar(32);not null"`
	Reference   string `json:"reference" gorm:"type:varchar(256);not null"`
	ActorID     int    `json:"actor_id" gorm:"not null"`
	Fingerprint string `json:"-" gorm:"type:varchar(64);not null"`
	CreatedAt   int64  `json:"created_at" gorm:"type:bigint;not null"`
}

func (*BillingUpstreamCost) BeforeUpdate(*gorm.DB) error {
	return errors.New("confirmed upstream costs are immutable")
}

func (*BillingUpstreamCost) BeforeDelete(*gorm.DB) error {
	return errors.New("confirmed upstream costs are immutable")
}

type BillingCostCommand struct {
	EventID   string `json:"event_id"`
	AttemptID string `json:"attempt_id"`
	AmountUSD string `json:"amount_usd"`
	Source    string `json:"source"`
	Reference string `json:"reference"`
}

type BillingCostSummary struct {
	AttemptCount          int                   `json:"attempt_count"`
	ConfirmedAttemptCount int                   `json:"confirmed_attempt_count"`
	Complete              bool                  `json:"complete"`
	UpstreamCostUSD       *string               `json:"upstream_cost_usd"`
	Records               []BillingUpstreamCost `json:"records"`
}

func parseBillingCostAmount(value string) (decimal.Decimal, error) {
	if len(value) == 0 || len(value) > 64 {
		return decimal.Zero, ErrBillingCommandInvalid
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || amount.IsNegative() || amount.Exponent() < -18 || amount.Exponent() > 18 {
		return decimal.Zero, ErrBillingCommandInvalid
	}
	if len(amount.String()) > 64 {
		return decimal.Zero, ErrBillingCommandInvalid
	}
	return amount, nil
}

func validBillingCostIdentifier(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// References are short, redacted invoice/receipt labels, not original evidence
// documents, URLs, credentials, or request/response payloads.
func validBillingCostReference(value string) bool {
	if !validBillingCostIdentifier(value, 256) {
		return false
	}
	if strings.ContainsAny(value, "{}<>@=\\") || strings.Contains(value, "://") {
		return false
	}
	for _, marker := range []string{"bearer ", "authorization", "api_key", "api-key", "password", "secret", "private key"} {
		if strings.Contains(strings.ToLower(value), marker) {
			return false
		}
	}
	return true
}

func billingCostFingerprint(requestID string, actor int, command BillingCostCommand) (string, error) {
	return billingEventFingerprint(struct {
		RequestID string
		ActorID   int
		Command   BillingCostCommand
	}{requestID, actor, command})
}

func RecordBillingUpstreamCost(ctx context.Context, requestID string, actor int, command BillingCostCommand) (*BillingUpstreamCost, error) {
	if actor <= 0 || !validBillingCostIdentifier(requestID, 128) || !validBillingCostIdentifier(command.EventID, 192) ||
		!validBillingCostIdentifier(command.AttemptID, 128) || !validBillingCostReference(command.Reference) ||
		(command.Source != BillingCostSourceInvoice && command.Source != BillingCostSourceReceipt) {
		return nil, ErrBillingCommandInvalid
	}
	amount, err := parseBillingCostAmount(command.AmountUSD)
	if err != nil {
		return nil, err
	}
	command.AmountUSD = amount.String()
	fingerprint, err := billingCostFingerprint(requestID, actor, command)
	if err != nil {
		return nil, err
	}
	var result *BillingUpstreamCost
	err = BillingTransaction(ctx, func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status != BillingRecoveryStatusSettled && recovery.Status != BillingRecoveryStatusRefunded {
			return ErrBillingRecoveryStateConflict
		}
		var attempt BillingAttempt
		if err := tx.Where("id = ?", command.AttemptID).First(&attempt).Error; err != nil {
			return err
		}
		if attempt.RequestID != requestID {
			return ErrBillingEventConflict
		}
		cost := BillingUpstreamCost{EventID: command.EventID, RequestID: requestID, AttemptID: command.AttemptID,
			AmountUSD: command.AmountUSD, Source: command.Source, Reference: command.Reference, ActorID: actor,
			Fingerprint: fingerprint, CreatedAt: common.GetTimestamp()}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cost)
		if insert.Error != nil {
			return insert.Error
		}
		// Read the persisted row even after insertion: MySQL's found-rows
		// option can report a no-op conflict as one affected row.
		var existing BillingUpstreamCost
		err := tx.Where("event_id = ?", command.EventID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && existing.Fingerprint != fingerprint {
			return ErrBillingEventConflict
		}
		if err != nil {
			return err
		}
		result = &existing
		return nil
	})
	return result, err
}

// Missing evidence stays unknown, including an empty attempt set. A failed
// attempt with no charge still needs an explicit zero-cost confirmation.
func ReadBillingCostSummary(ctx context.Context, requestID string) (BillingCostSummary, error) {
	result := BillingCostSummary{Records: make([]BillingUpstreamCost, 0)}
	if DB == nil {
		return result, errors.New("billing database is unavailable")
	}
	if !validBillingCostIdentifier(requestID, 128) {
		return result, ErrBillingCommandInvalid
	}
	q := DB.WithContext(ctx)
	var recovery BillingRecovery
	if err := q.Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return result, err
	}
	var attempts []BillingAttempt
	if err := q.Where("request_id = ?", requestID).Find(&attempts).Error; err != nil {
		return result, err
	}
	if err := q.Where("request_id = ?", requestID).Order("created_at, id").Find(&result.Records).Error; err != nil {
		return result, err
	}
	result.AttemptCount, result.ConfirmedAttemptCount = len(attempts), len(result.Records)
	known := make(map[string]bool, len(attempts))
	for _, attempt := range attempts {
		known[attempt.ID] = true
	}
	total := decimal.Zero
	for _, cost := range result.Records {
		if !known[cost.AttemptID] {
			return result, fmt.Errorf("billing cost attempt integrity mismatch")
		}
		delete(known, cost.AttemptID)
		amount, err := parseBillingCostAmount(cost.AmountUSD)
		if err != nil {
			return result, fmt.Errorf("invalid confirmed billing cost amount")
		}
		fingerprint, err := billingCostFingerprint(cost.RequestID, cost.ActorID, BillingCostCommand{EventID: cost.EventID, AttemptID: cost.AttemptID, AmountUSD: amount.String(), Source: cost.Source, Reference: cost.Reference})
		if err != nil {
			return result, err
		}
		if fingerprint != cost.Fingerprint {
			return result, fmt.Errorf("billing cost evidence integrity mismatch")
		}
		total = total.Add(amount)
	}
	result.Complete = result.AttemptCount > 0 && len(known) == 0 &&
		(recovery.Status == BillingRecoveryStatusSettled || recovery.Status == BillingRecoveryStatusRefunded)
	if result.Complete {
		amount := total.String()
		result.UpstreamCostUSD = &amount
	}
	return result, nil
}
