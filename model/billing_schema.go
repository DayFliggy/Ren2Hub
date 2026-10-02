package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const BillingSchemaVersion = 5

type BillingSchema struct {
	ID            int    `gorm:"primaryKey"`
	Version       int    `gorm:"not null"`
	EvidenceKeyID string `gorm:"type:varchar(64);not null"`
}

// This release is an installation format, not an upgrade migration. The
// marker is created before application tables so interrupted empty installs
// can resume without allowing an old accounting database to be reinterpreted.
func prepareBillingSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("billing database unavailable")
	}
	cipher, err := billingEvidenceCipher()
	if err != nil {
		return err
	}
	if db.Migrator().HasTable(&BillingSchema{}) {
		var count int64
		if err := db.Model(&BillingSchema{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return requireBillingSchema(db)
		}
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return err
	}
	for _, table := range tables {
		if table == "billing_schemas" || table == "sqlite_sequence" || table == "sqlite_stat1" {
			continue
		}
		return errors.New("this billing release requires an empty database; existing databases are not upgraded or erased")
	}
	if err := db.AutoMigrate(&BillingSchema{}); err != nil {
		return err
	}
	return db.Create(&BillingSchema{ID: 1, Version: BillingSchemaVersion, EvidenceKeyID: cipher.KeyID()}).Error
}

// A process that does not initialize tables must still reject old databases.
func requireBillingSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&BillingSchema{}) {
		return errors.New("billing schema is not initialized; existing databases cannot be upgraded")
	}
	var schema BillingSchema
	if err := db.First(&schema, 1).Error; err != nil {
		return fmt.Errorf("invalid billing schema marker: %w", err)
	}
	if schema.Version != BillingSchemaVersion {
		return errors.New("unsupported billing schema version")
	}
	cipher, err := billingEvidenceCipher()
	if err != nil {
		return err
	}
	if schema.EvidenceKeyID == "" || schema.EvidenceKeyID != cipher.KeyID() {
		return errors.New("billing encryption key does not match this database")
	}
	// Management tokens are replaced only on a fresh installation. Refuse the
	// previous user-row format before AutoMigrate can silently change the schema.
	if db.Migrator().HasColumn("users", "access_token") {
		return errors.New("legacy management token schema is unsupported; initialize a separate empty database")
	}
	return requireSupportedBillingRecoveryStates(db)
}

// Schema 5 predates automatic finalization. Retired states cannot be silently
// left outside recovery or reinterpreted as permission to charge or release.
func requireSupportedBillingRecoveryStates(db *gorm.DB) error {
	if !db.Migrator().HasTable(&BillingRecovery{}) {
		return nil // Interrupted empty installations may not have this table yet.
	}
	var incompatible int64
	err := db.Model(&BillingRecovery{}).
		Where("status IS NULL OR status NOT IN ? OR consumption_state IN ? OR (status IN ? AND (consumption_state IS NULL OR consumption_state NOT IN ?))",
			[]string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending, BillingRecoveryStatusRefundPending, BillingRecoveryStatusSettled, BillingRecoveryStatusRefunded},
			[]string{"pricing_pending", "pricing_collect"},
			[]string{BillingRecoveryStatusSettled, BillingRecoveryStatusRefunded},
			[]string{BillingRecoveryStatePending, BillingRecoveryStateCompleted}).
		Count(&incompatible).Error
	if err != nil {
		return err
	}
	if incompatible != 0 {
		return errors.New("unsupported billing recovery states; finish recovery with the previous release before upgrading")
	}
	return nil
}
