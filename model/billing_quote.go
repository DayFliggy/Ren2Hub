package model

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

// ValidatePublishedBillingQuote checks the frozen definition against its
// immutable database version. It deliberately does not consult the live active
// pointer: a published replacement cannot invalidate an accepted attempt.
func ValidatePublishedBillingQuote(ctx context.Context, quote *billing.Quote) error {
	if DB == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	return validatePublishedBillingQuoteTx(DB.WithContext(ctx), quote)
}

func validatePublishedBillingQuoteTx(tx *gorm.DB, quote *billing.Quote) error {
	if quote == nil {
		return fmt.Errorf("published billing quote is required")
	}
	if err := validateBillingAuditQuote(*quote); err != nil {
		return err
	}
	var row BillingPriceVersion
	if err := tx.First(&row, quote.PriceVersion.ID).Error; err != nil {
		return fmt.Errorf("published billing price is missing: %w", err)
	}
	published, err := decodeBillingPrice(row)
	if err != nil {
		return err
	}
	publishedHash, err := billing.Hash(published)
	if err != nil {
		return err
	}
	versionHash, err := billing.Hash(quote.PriceVersion)
	if err != nil {
		return err
	}
	if publishedHash != versionHash {
		return ErrBillingEventConflict
	}
	return nil
}
