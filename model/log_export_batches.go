package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// LogExportBatchSize bounds the rows retained while writing to a slow client.
const LogExportBatchSize = 500
const logExportQueryTimeout = 30 * time.Second

func streamLogBatches(tx *gorm.DB, order string, fn func(*Log) error) error {
	ctx := tx.Statement.Context
	// Freeze the auto-increment watermark independently of created_at: backdated
	// inserts made during the export must not enter later batches.
	queryCtx, cancel := context.WithTimeout(ctx, logExportQueryTimeout)
	var upperID int
	err := tx.WithContext(queryCtx).Model(&Log{}).Select("COALESCE(MAX(logs.id), 0)").Scan(&upperID).Error
	cancel()
	if err != nil {
		return err
	}
	var lastID int
	var lastCreated int64
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		query := tx.Session(&gorm.Session{}).Where("logs.id <= ?", upperID)
		if !first {
			if order == "logs.id desc" {
				query = query.Where("logs.id < ?", lastID)
			} else {
				query = query.Where("(logs.created_at < ? OR (logs.created_at = ? AND logs.id < ?))", lastCreated, lastCreated, lastID)
			}
		}
		queryCtx, cancel = context.WithTimeout(ctx, logExportQueryTimeout)
		var batch []*Log
		err = query.WithContext(queryCtx).Order(order).Limit(LogExportBatchSize).Find(&batch).Error
		cancel()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		// Find closes its cursor before any callback; capture the real key before
		// StreamUserLogs replaces IDs with display positions.
		last := batch[len(batch)-1]
		lastID, lastCreated = last.Id, last.CreatedAt
		first = false
		for _, log := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := fn(log); err != nil {
				return err
			}
		}
		if len(batch) < LogExportBatchSize {
			return nil
		}
	}
}
