package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type TaskSettlementLogReceipt struct {
	TaskID int64 `gorm:"primaryKey;autoIncrement:false"`
}

func DeliverTaskSettlementLog(ctx context.Context, taskID int64) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var receipt TaskSettlement
		if err := lockForUpdate(tx).First(&receipt, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if !receipt.Applied {
			return errors.New("task settlement has not committed")
		}
		if receipt.LogDelivered {
			return nil
		}
		var params RecordTaskBillingLogParams
		if err := common.UnmarshalJsonStr(receipt.LogPayload, &params); err != nil {
			return err
		}
		var user User
		if err := tx.Unscoped().Select("username").First(&user, receipt.UserID).Error; err != nil {
			return err
		}
		logType, quota := LogTypeConsume, receipt.Delta
		if quota < 0 {
			logType, quota = LogTypeRefund, -quota
		}
		log := Log{UserId: receipt.UserID, Username: user.Username, ChannelId: params.ChannelId,
			TokenId: receipt.TokenID, ModelName: params.ModelName, CreatedAt: receipt.CreatedAt,
			Quota: quota, Type: logType, Content: params.Content, RequestId: fmt.Sprintf("task-settlement:%d", taskID)}
		if receipt.TokenID > 0 && !receipt.TokenMissing {
			var token Token
			err := tx.Unscoped().Select("name").First(&token, receipt.TokenID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			log.TokenName = token.Name
		}
		other := params.Other
		if other == nil {
			other = make(map[string]any)
		}
		other["settlement_operation"] = receipt.Operation
		other["pre_consumed_quota"], other["actual_quota"] = receipt.BeforeQuota, receipt.FinalQuota
		if receipt.TokenMissing {
			admin, _ := other["admin_info"].(map[string]any)
			if admin == nil {
				admin = make(map[string]any)
			}
			admin["token_accounting"] = "physically_missing"
			other["admin_info"] = admin
		}
		payload, err := common.Marshal(other)
		if err != nil {
			return err
		}
		log.Other = string(payload)
		if receipt.LogRequired {
			logDB := LOG_DB.WithContext(ctx)
			if LOG_DB == DB {
				logDB = tx
			}
			if err := deliverTaskLogTx(logDB, &log, taskID); err != nil {
				return err
			}
		}
		return tx.Model(&receipt).Update("log_delivered", true).Error
	})
}

func deliverTaskLogTx(db *gorm.DB, log *Log, taskID int64) error {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return errors.New("task settlement log delivery requires ClickHouse deduplication support")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var receipt TaskSettlementLogReceipt
		err := tx.First(&receipt, "task_id = ?", taskID).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&TaskSettlementLogReceipt{TaskID: taskID}).Error; err != nil {
			return err
		}
		return tx.Create(log).Error
	})
}
