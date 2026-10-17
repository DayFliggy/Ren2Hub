package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// The upstream wallet cannot interpret the retired Ren2Hub funds ledger.
// Reject it before AutoMigrate rather than partially converting a live archive.
func rejectLegacyBillingDatabase(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is not initialized")
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return fmt.Errorf("inspect database before migration: %w", err)
	}
	for _, table := range tables {
		if table == "billing_schemas" || table == "billing_events" {
			return errors.New("legacy Ren2Hub billing database is unsupported; initialize a separate empty database")
		}
	}
	return nil
}
