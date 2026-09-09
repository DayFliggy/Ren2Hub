package model

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// FlushOperation is committed with a frozen batch so a lost acknowledgement
// can be retried without applying the same increments twice.
type FlushOperation struct {
	ID        string `gorm:"primaryKey;size:64"`
	CreatedAt int64  `gorm:"index"`
	Applied   bool
}

func ApplyFlushOperation(ctx context.Context, id string, apply func(*gorm.DB) error) error {
	if DB == nil || id == "" {
		return errors.New("flush database and operation ID are required")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		receipt := FlushOperation{ID: id, CreatedAt: time.Now().Unix()}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
		if result.Error != nil {
			return result.Error
		}
		// MySQL CLIENT_FOUND_ROWS can report one row for a duplicate no-op.
		// Read the locked receipt instead of interpreting affected-row counts.
		var stored FlushOperation
		if err := lockForUpdate(tx).First(&stored, "id = ?", id).Error; err != nil {
			return err
		}
		if stored.Applied {
			return nil
		}
		if err := apply(tx); err != nil {
			return err
		}
		return tx.Model(&FlushOperation{}).Where("id = ?", id).Update("applied", true).Error
	})
}
