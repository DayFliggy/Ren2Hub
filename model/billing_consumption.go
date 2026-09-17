package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BillingConsumption contains only the metadata already intended for the log.
// The snapshot and settlement intent are committed before any final charge.
type BillingConsumption struct {
	Log            Log                    `json:"log"`
	CountUsage     bool                   `json:"count_usage"`
	LogEnabled     bool                   `json:"log_enabled"`
	ExportEnabled  bool                   `json:"export_enabled"`
	NodeName       string                 `json:"node_name"`
	TaskAdjustment *TaskBillingAdjustment `json:"task_adjustment,omitempty"`
}

func PrepareBillingRecoveryConsumption(requestID string, consumption BillingConsumption) error {
	return prepareBillingRecoveryConsumption(DB, requestID, consumption, true)
}

// A live Realtime session may keep replacing its cumulative snapshot until
// final settlement. A stale session recovers this last confirmed consumption.
func RecordRealtimeBillingConsumption(requestID string, consumption BillingConsumption) error {
	return prepareBillingRecoveryConsumption(DB, requestID, consumption, false)
}

func prepareBillingRecoveryConsumption(db *gorm.DB, requestID string, consumption BillingConsumption, final bool) error {
	if db == nil || requestID == "" || consumption.Log.UserId <= 0 || consumption.Log.Quota < 0 || consumption.TaskAdjustment != nil {
		return errors.New("invalid billing consumption")
	}
	consumption.Log.RequestId = requestID
	data, err := common.Marshal(consumption)
	if err != nil {
		return fmt.Errorf("encode billing consumption: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.UserID != consumption.Log.UserId || recovery.TokenID != consumption.Log.TokenId {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.ConsumptionSnapshot != "" && recovery.ConsumptionState != BillingRecoveryStateCollecting &&
			recovery.ConsumptionState != BillingRecoveryStatePricingPending && recovery.ConsumptionState != BillingRecoveryStatePricingCollecting {
			var existing BillingConsumption
			if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &existing); err != nil {
				return err
			}
			if existing.Log.Quota != consumption.Log.Quota || existing.Log.ChannelId != consumption.Log.ChannelId ||
				existing.Log.ModelName != consumption.Log.ModelName || existing.CountUsage != consumption.CountUsage ||
				existing.Log.PromptTokens != consumption.Log.PromptTokens || existing.Log.CompletionTokens != consumption.Log.CompletionTokens {
				return ErrBillingRecoveryStateConflict
			}
			return nil
		}
		reserved := recovery.FundingAmount
		if recovery.Source == "subscription" {
			reserved += recovery.ExtraAmount
		}
		delta := int64(consumption.Log.Quota) - reserved
		switch recovery.Status {
		case BillingRecoveryStatusActive:
		case BillingRecoveryStatusSettlementPending, BillingRecoveryStatusSettled:
			if recovery.SettlementDelta != delta {
				return ErrBillingRecoveryStateConflict
			}
		default:
			return ErrBillingRecoveryStateConflict
		}
		status := recovery.Status
		state := BillingRecoveryStatePending
		if !final {
			if status != BillingRecoveryStatusActive {
				return ErrBillingRecoveryStateConflict
			}
			state = BillingRecoveryStateCollecting
			if recovery.ConsumptionState == BillingRecoveryStatePricingPending || recovery.ConsumptionState == BillingRecoveryStatePricingCollecting {
				// Further Realtime output can still fail expression evaluation.
				state = BillingRecoveryStatePricingCollecting
			}
		} else if status == BillingRecoveryStatusActive {
			status = BillingRecoveryStatusSettlementPending
		}
		return tx.Model(&recovery).Updates(map[string]any{
			"consumption_snapshot":   string(data),
			"consumption_state":      state,
			"consumption_statistics": BillingRecoveryStatePending,
			"consumption_log":        BillingRecoveryStatePending,
			"settlement_delta":       delta,
			"status":                 status,
			"updated_at":             common.GetTimestamp(),
		}).Error
	})
}

// ApplyBillingRecoveryConsumption can be repeated after any process or database
// failure. Main-database counters share a transaction with their completion bit;
// an independent SQL log database uses a nullable unique billing request key.
func ApplyBillingRecoveryConsumption(requestID string) error {
	if DB == nil || requestID == "" {
		return errors.New("invalid billing consumption recovery")
	}
	err := applyBillingConsumptionStatistics(requestID)
	if err == nil {
		err = applyBillingConsumptionLog(requestID)
	}
	if err != nil {
		writeErr := DB.Model(&BillingRecovery{}).
			Where("request_id = ? AND consumption_state IN ?", requestID, []string{BillingRecoveryStatePending, BillingRecoveryStateUnprepared, BillingRecoveryStatePricingPending, BillingRecoveryStatePricingCollecting}).
			Updates(map[string]any{
				"consumption_error": err.Error(),
				"attempts":          gorm.Expr("attempts + 1"),
				"updated_at":        common.GetTimestamp(),
			}).Error
		return errors.Join(err, writeErr)
	}
	return nil
}

// Preserve an explicit reconciliation record if no complete snapshot could be
// persisted. Recovery must not silently treat the monetary settlement as done.
func RecordBillingConsumptionPreparationFailure(requestID string, cause error) error {
	if DB == nil || requestID == "" || cause == nil {
		return errors.New("invalid billing consumption preparation failure")
	}
	return DB.Model(&BillingRecovery{}).
		Where("request_id = ? AND (consumption_snapshot = '' OR consumption_snapshot IS NULL)", requestID).
		Updates(map[string]any{
			"consumption_state": BillingRecoveryStateUnprepared, "consumption_error": cause.Error(),
			"attempts": gorm.Expr("attempts + 1"), "updated_at": common.GetTimestamp(),
		}).Error
}

func applyBillingConsumptionStatistics(requestID string) error {
	// Legacy task statistics use this cache lock. Keep their flush from racing
	// with the durable gateway update of the same hourly bucket in this process.
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.ConsumptionState == BillingRecoveryStatePricingPending || recovery.ConsumptionState == BillingRecoveryStatePricingCollecting {
			return errors.New("billing expression consumption is unconfirmed; reconciliation is required")
		}
		if recovery.ConsumptionSnapshot == "" && recovery.ConsumptionState == BillingRecoveryStateUnprepared {
			return errors.New("billing consumption snapshot is missing; reconciliation is required")
		}
		if recovery.ConsumptionSnapshot == "" || recovery.ConsumptionStatistics == BillingRecoveryStateCompleted {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusSettled {
			return ErrBillingRecoveryStateConflict
		}
		var consumption BillingConsumption
		if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption); err != nil {
			return err
		}
		log := consumption.Log
		quotaDelta, requestDelta := log.Quota, 1
		if adjustment := consumption.TaskAdjustment; adjustment != nil {
			if adjustment.PreviousQuota < 0 || adjustment.PreviousQuota > common.MaxQuota ||
				adjustment.FinalQuota < 0 || adjustment.FinalQuota > common.MaxQuota ||
				(adjustment.TaskID > 0) == (adjustment.MidjourneyID > 0) {
				return ErrBillingRecoveryStateConflict
			}
			quotaDelta, requestDelta = adjustment.FinalQuota-adjustment.PreviousQuota, 0
			var target any = &Task{}
			taskID := adjustment.TaskID
			if adjustment.MidjourneyID > 0 {
				target, taskID = &Midjourney{}, int64(adjustment.MidjourneyID)
			}
			result := tx.Model(target).Where("id = ? AND user_id = ? AND quota = ?", taskID, recovery.UserID, adjustment.PreviousQuota).
				Update("quota", adjustment.FinalQuota)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 && quotaDelta != 0 {
				return ErrBillingRecoveryStateConflict
			}
		}
		if consumption.CountUsage || consumption.ExportEnabled {
			var user User
			if err := lockForUpdate(tx).Select("id").Where("id = ?", recovery.UserID).First(&user).Error; err != nil {
				return err
			}
		}
		if consumption.CountUsage {
			if err := tx.Model(&User{}).Where("id = ?", recovery.UserID).Updates(map[string]any{
				"used_quota":    gorm.Expr("used_quota + ?", quotaDelta),
				"request_count": gorm.Expr("request_count + ?", requestDelta),
			}).Error; err != nil {
				return err
			}
			if log.ChannelId > 0 {
				var channel Channel
				if err := lockForUpdate(tx).Select("id").Where("id = ?", log.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				if quotaDelta != 0 {
					if err := tx.Model(&channel).Update("used_quota", gorm.Expr("used_quota + ?", quotaDelta)).Error; err != nil {
						return err
					}
				}
			}
		}
		if consumption.ExportEnabled {
			bucket := QuotaData{
				UserID: recovery.UserID, Username: log.Username, ModelName: log.ModelName,
				CreatedAt: log.CreatedAt - log.CreatedAt%3600, UseGroup: log.Group,
				TokenID: log.TokenId, ChannelID: log.ChannelId, NodeName: consumption.NodeName,
				Count: requestDelta, Quota: quotaDelta, TokenUsed: log.PromptTokens + log.CompletionTokens,
			}
			var existing QuotaData
			err := tx.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ? AND use_group = ? AND token_id = ? AND channel_id = ? AND node_name = ?",
				bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt, bucket.UseGroup, bucket.TokenID, bucket.ChannelID, bucket.NodeName).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = tx.Create(&bucket).Error
			} else if err == nil {
				err = tx.Model(&existing).Updates(map[string]any{
					"count": gorm.Expr("count + ?", bucket.Count), "quota": gorm.Expr("quota + ?", bucket.Quota),
					"token_used": gorm.Expr("token_used + ?", bucket.TokenUsed),
				}).Error
			}
			if err != nil {
				return err
			}
		}
		return tx.Model(&recovery).Updates(map[string]any{
			"consumption_statistics": BillingRecoveryStateCompleted, "updated_at": common.GetTimestamp(),
		}).Error
	})
}

func applyBillingConsumptionLog(requestID string) error {
	var recovery BillingRecovery
	if err := DB.Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	if recovery.ConsumptionSnapshot == "" || recovery.ConsumptionState == BillingRecoveryStateCompleted {
		return nil
	}
	if recovery.Status != BillingRecoveryStatusSettled || recovery.ConsumptionStatistics != BillingRecoveryStateCompleted {
		return ErrBillingRecoveryStateConflict
	}
	var consumption BillingConsumption
	if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption); err != nil {
		return err
	}
	if consumption.LogEnabled && recovery.ConsumptionLog != BillingRecoveryStateCompleted {
		if LOG_DB == nil {
			return errors.New("billing log database is unavailable")
		}
		log := consumption.Log
		log.BillingRequestID = &requestID
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			var count int64
			if err := LOG_DB.Model(&Log{}).Where("request_id = ? AND type = ?", requestID, log.Type).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				// MergeTree has no unique constraint. A persisted claim prevents
				// replay after an ambiguous insert; recovery only confirms visibility.
				claim := DB.Model(&BillingRecovery{}).Where("request_id = ? AND consumption_log = ?", requestID, BillingRecoveryStatePending).
					Update("consumption_log", BillingRecoveryStateWriting)
				if claim.Error != nil {
					return claim.Error
				}
				if claim.RowsAffected != 1 {
					return errors.New("ClickHouse consumption insert outcome is unknown; reconciliation is required")
				}
				if err := createLog(&log); err != nil {
					return fmt.Errorf("ClickHouse consumption insert outcome is unknown: %w", err)
				}
			}
		} else if err := LOG_DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "billing_request_id"}}, DoNothing: true,
		}).Create(&log).Error; err != nil {
			return err
		}
	}
	return DB.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
		"consumption_log":   BillingRecoveryStateCompleted,
		"consumption_state": BillingRecoveryStateCompleted,
		"consumption_error": "", "updated_at": common.GetTimestamp(),
	}).Error
}
