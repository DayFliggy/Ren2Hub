package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BillingConsumption contains only the metadata already intended for the log.
// The snapshot and settlement intent are committed before any final charge.
type BillingConsumption struct {
	Usage          *billing.Usage         `json:"-"`
	Charge         *billing.ChargeResult  `json:"-"`
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
	usage, err := common.Marshal(consumption.Usage)
	if err != nil {
		return err
	}
	charge, err := common.Marshal(consumption.Charge)
	if err != nil {
		return err
	}
	if consumption.Charge != nil && consumption.Charge.FinalQuota != common.Quota(consumption.Log.Quota) {
		return ErrBillingEventConflict
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.UserID != consumption.Log.UserId || recovery.TokenID != consumption.Log.TokenId {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.ConsumptionSnapshot != "" && !BillingRecoveryUsageCollecting(recovery.ConsumptionState) &&
			!BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
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
		if recovery.Source != "wallet" {
			return ErrUnsupportedBillingSource
		}
		delta := common.Quota(consumption.Log.Quota) - reserved
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
			} else if BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
				state = BillingRecoveryStateUsageCollecting
			}
		} else if status == BillingRecoveryStatusActive {
			status = BillingRecoveryStatusSettlementPending
		}
		updates := map[string]any{
			"version":                recovery.Version + 1,
			"consumption_snapshot":   string(data),
			"consumption_state":      state,
			"consumption_statistics": BillingRecoveryStatePending,
			"consumption_log":        BillingRecoveryStatePending,
			"settlement_delta":       delta,
			"status":                 status,
			"updated_at":             common.GetTimestamp(),
		}
		// Recovery decodes the display outbox independently. Absent internal
		// evidence must not erase the last persisted cumulative usage or charge.
		if consumption.Usage != nil {
			updates["usage_snapshot"] = string(usage)
		}
		if consumption.Charge != nil {
			updates["charge_snapshot"] = string(charge)
		}
		return tx.Model(&recovery).Updates(updates).Error
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
			Where("request_id = ? AND consumption_state IN ?", requestID, append(billingRecoveryUnconfirmedStates(), BillingRecoveryStatePending)).
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
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.ConsumptionSnapshot != "" && !BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
			return nil
		}
		state := recovery.ConsumptionState
		if !BillingRecoveryUsageUnconfirmed(state) {
			state = BillingRecoveryStateUnprepared
		}
		return tx.Model(&recovery).Updates(map[string]any{
			"consumption_state": state, "consumption_error": cause.Error(),
			"attempts": gorm.Expr("attempts + 1"), "updated_at": common.GetTimestamp(),
		}).Error
	})
}

func applyBillingConsumptionStatistics(requestID string) error {
	return BillingTransaction(context.Background(), func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
			return errors.New("billing consumption is unconfirmed; reconciliation is required")
		}
		if recovery.ConsumptionSnapshot == "" || recovery.ConsumptionStatistics == BillingRecoveryStateCompleted {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusSettled && recovery.Status != BillingRecoveryStatusRefunded {
			return ErrBillingRecoveryStateConflict
		}
		var consumption BillingConsumption
		if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption); err != nil {
			return err
		}
		log := consumption.Log
		quotaDelta, requestDelta := log.Quota, 1
		if !consumption.CountUsage {
			requestDelta = 0
		}
		if adjustment := consumption.TaskAdjustment; adjustment != nil {
			if adjustment.PreviousQuota < 0 || adjustment.PreviousQuota > common.MaxQuota ||
				adjustment.FinalQuota < 0 || adjustment.FinalQuota > common.MaxQuota ||
				(adjustment.TaskID > 0) == (adjustment.MidjourneyID > 0) {
				return ErrBillingRecoveryStateConflict
			}
			quotaDelta = adjustment.FinalQuota
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
			if result.RowsAffected == 0 {
				if adjustment.PreviousQuota != adjustment.FinalQuota {
					return ErrBillingRecoveryStateConflict
				}
				// MySQL reports changed rows; an unchanged final amount still
				// needs an existing, correctly owned task before statistics commit.
				var count int64
				if err := tx.Model(target).Where("id = ? AND user_id = ? AND quota = ?", taskID, recovery.UserID, adjustment.FinalQuota).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return ErrBillingRecoveryStateConflict
				}
			}
		}
		// Failed tasks release their reservation without a consumed request;
		// exported counters must reflect the same committed usage decision.
		if !consumption.CountUsage {
			requestDelta = 0
		}
		if consumption.CountUsage || consumption.ExportEnabled {
			var user User
			if err := lockForUpdate(tx).Select("id").Where("id = ?", recovery.UserID).First(&user).Error; err != nil {
				return err
			}
		}
		if consumption.CountUsage {
			if err := tx.Model(&User{}).Where("id = ?", recovery.UserID).Updates(map[string]any{

				"request_count": gorm.Expr("request_count + ?", requestDelta),
			}).Error; err != nil {
				return err
			}
			if log.ChannelId > 0 {
				var channel Channel
				if err := lockForUpdate(tx).Select("id", "used_quota").Where("id = ?", log.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				if quotaDelta != 0 {
					total, err := common.AddQuota(channel.UsedQuota, quotaDelta)
					if err != nil {
						return err
					}
					if err := tx.Model(&channel).Update("used_quota", total).Error; err != nil {
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
				total, addErr := common.AddQuota(existing.Quota, bucket.Quota)
				if addErr != nil {
					return addErr
				}
				err = tx.Model(&existing).Updates(map[string]any{
					"count": gorm.Expr("count + ?", bucket.Count), "quota": total,
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
	if (recovery.Status != BillingRecoveryStatusSettled && recovery.Status != BillingRecoveryStatusRefunded) || recovery.ConsumptionStatistics != BillingRecoveryStateCompleted {
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
		if recovery.FinalEventID == "" {
			return ErrBillingRecoveryStateConflict
		}
		log.BillingEventID = &recovery.FinalEventID
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			var count int64
			if err := LOG_DB.Model(&Log{}).Where("billing_event_id = ?", recovery.FinalEventID).Count(&count).Error; err != nil {
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
			Columns: []clause.Column{{Name: "billing_event_id"}}, DoNothing: true,
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
