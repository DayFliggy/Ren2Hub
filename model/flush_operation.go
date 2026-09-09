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
		if result.RowsAffected == 0 {
			return nil
		}
		return apply(tx)
	})
}
