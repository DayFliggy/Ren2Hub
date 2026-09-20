package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const BillingSchemaVersion = 1

type BillingSchema struct {
	ID      int `gorm:"primaryKey"`
	Version int `gorm:"not null"`
}

// This release is an installation format, not an upgrade migration. The
// marker is created before application tables so interrupted empty installs
// can resume without allowing an old accounting database to be reinterpreted.
func prepareBillingSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("billing database unavailable")
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
	return db.Create(&BillingSchema{ID: 1, Version: BillingSchemaVersion}).Error
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
	return nil
}
