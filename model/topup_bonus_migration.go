package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// The top-up discount became the top-up bonus. Startup moves its data into the
// topup_bonus tables and drops every discount table, so nothing of the old
// contract remains. Each step can be interrupted and rerun: it resumes from
// the remaining source rows and never counts a row twice.

const (
	legacyTopUpDiscountConfigTable = "topup_discount_configs"
	legacyTopUpDiscountUsageTable  = "topup_discount_usage_hourly"
	topUpBonusUsageMoveBatch       = 1000
)

// legacyTopUpDiscountTables hold nothing the bonus keeps: frozen discount
// explanations and the empty tables of the earlier usage discount.
var legacyTopUpDiscountTables = []string{"topup_discounts", "usage_discount_configs", "usage_discount_hourly"}

// migrateTopUpBonusTables runs after AutoMigrate has created the bonus tables.
func migrateTopUpBonusTables(db *gorm.DB) error {
	if err := migrateTopUpBonusConfig(db); err != nil {
		return fmt.Errorf("migrate top-up bonus config: %w", err)
	}
	if err := moveTopUpBonusUsage(db); err != nil {
		return fmt.Errorf("move top-up bonus usage: %w", err)
	}
	for _, table := range legacyTopUpDiscountTables {
		if err := dropTableIfExists(db, table); err != nil {
			return err
		}
	}
	return nil
}

// migrateTopUpBonusConfig keeps the rates of the latest discount version
// unchanged: it becomes bonus version 1 with its original author and time.
// A bonus table that already has versions is authoritative and kept as is.
// The source is dropped only after the bonus table loads and validates.
func migrateTopUpBonusConfig(db *gorm.DB) error {
	exists, err := legacyTableExists(db, legacyTopUpDiscountConfigTable)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	var count int64
	if err := db.Model(&TopUpBonusConfigVersion{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		var rows []TopUpBonusConfigVersion
		if err := db.Table(legacyTopUpDiscountConfigTable).Order("version DESC").Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 1 {
			if _, err := ParseTopUpBonusConfig([]byte(rows[0].Config)); err != nil {
				return fmt.Errorf("discount config version %d: %w", rows[0].Version, err)
			}
			if err := db.Create(&TopUpBonusConfigVersion{
				Version: 1, Config: rows[0].Config, CreatedBy: rows[0].CreatedBy, CreatedAt: rows[0].CreatedAt,
			}).Error; err != nil {
				return err
			}
		}
	}
	if _, err := loadTopUpBonusSettings(db); err != nil {
		return err
	}
	return dropTableIfExists(db, legacyTopUpDiscountConfigTable)
}

// moveTopUpBonusUsage adds every legacy hourly row to the bonus table. Each
// batch adds its rows and deletes exactly those rows in one transaction, so a
// rerun only sees rows not yet moved. Rows written by a rolled-back release
// are new usage and add to the same hours.
func moveTopUpBonusUsage(db *gorm.DB) error {
	exists, err := legacyTableExists(db, legacyTopUpDiscountUsageTable)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	for {
		moved, err := moveTopUpBonusUsageBatch(db)
		if err != nil {
			return err
		}
		if moved < topUpBonusUsageMoveBatch {
			break
		}
	}
	var remaining int64
	if err := db.Table(legacyTopUpDiscountUsageTable).Count(&remaining).Error; err != nil {
		return err
	}
	if remaining != 0 {
		return fmt.Errorf("%d legacy usage rows were not moved", remaining)
	}
	return dropTableIfExists(db, legacyTopUpDiscountUsageTable)
}

func moveTopUpBonusUsageBatch(db *gorm.DB) (int, error) {
	var rows []TopUpBonusUsageHourly
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Table(legacyTopUpDiscountUsageTable).Order("hour_utc, user_id").
			Limit(topUpBonusUsageMoveBatch).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := addTopUpBonusUsageHourly(tx, row); err != nil {
				return err
			}
			result := tx.Table(legacyTopUpDiscountUsageTable).Where("user_id = ? AND hour_utc = ?", row.UserID, row.HourUTC).
				Delete(&TopUpBonusUsageHourly{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("legacy usage row user_id=%d hour_utc=%d changed during the move", row.UserID, row.HourUTC)
			}
		}
		return nil
	})
	return len(rows), err
}

// legacyTableExists reads the catalog itself because GORM's HasTable reports a
// failed query as a missing table, which would skip the move and still start.
func legacyTableExists(db *gorm.DB, table string) (bool, error) {
	var query string
	switch common.MainDatabaseType() {
	case common.DatabaseTypeSQLite:
		query = "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	case common.DatabaseTypeMySQL:
		query = "SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ? AND table_type = 'BASE TABLE'"
	case common.DatabaseTypePostgreSQL:
		query = "SELECT count(*) FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA() AND table_name = ? AND table_type = 'BASE TABLE'"
	default:
		return false, fmt.Errorf("unsupported main database type %q", common.MainDatabaseType())
	}
	var count int64
	if err := db.Raw(query, table).Scan(&count).Error; err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return count > 0, nil
}

// dropTableIfExists relies on DropTable issuing DROP TABLE IF EXISTS on all
// three databases, so a failure is always a real error.
func dropTableIfExists(db *gorm.DB, table string) error {
	if err := db.Migrator().DropTable(table); err != nil {
		return fmt.Errorf("drop %s: %w", table, err)
	}
	return nil
}
