package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PerfMetricFlushReceipt fences retries of an immutable in-memory metric batch.
// It is observability state, independent of wallet and task-result events.
type PerfMetricFlushReceipt struct {
	ID          string `gorm:"primaryKey;size:64"`
	Fingerprint string `gorm:"size:64;not null"`
	Applied     bool
	CreatedAt   int64 `gorm:"not null;index"`
}

func FlushPerfMetric(ctx context.Context, operationID string, metric *PerfMetric) error {
	if operationID == "" || len(operationID) > 64 || strings.TrimSpace(operationID) != operationID || metric == nil || metric.RequestCount <= 0 {
		return errors.New("invalid performance metric flush")
	}
	batch := *metric
	batch.Id = 0
	// PerfMetric JSON intentionally hides counters; hash the stored values
	// explicitly so a reused operation ID cannot silently change its payload.
	payload := []any{batch.ModelName, batch.Group, batch.BucketTs, batch.RequestCount, batch.SuccessCount, batch.TotalLatencyMs, batch.TtftSumMs, batch.TtftCount, batch.OutputTokens, batch.GenerationMs}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		receipt := PerfMetricFlushReceipt{ID: operationID, Fingerprint: fingerprint, CreatedAt: time.Now().Unix()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt).Error; err != nil {
			return err
		}
		var stored PerfMetricFlushReceipt
		if err := lockForUpdate(tx).First(&stored, "id = ?", operationID).Error; err != nil {
			return err
		}
		if stored.Fingerprint != fingerprint {
			return errors.New("performance metric flush identity conflict")
		}
		if stored.Applied {
			return nil
		}
		copy := batch
		if err := upsertPerfMetricWithTx(tx, &copy); err != nil {
			return err
		}
		return tx.Model(&stored).Update("applied", true).Error
	})
}
