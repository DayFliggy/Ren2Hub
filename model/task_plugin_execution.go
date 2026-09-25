package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskPluginRef = hosttypes.TaskPluginRef

// Namespace rows serialize version publication, activation and execution pins.
type TaskPluginNamespace struct {
	Key string `gorm:"type:varchar(128);primaryKey"`
}

// Executions are durable code references. Attaching a task retains its exact
// source for polling and artifact retrieval, including across process upgrades.
type TaskPluginExecution struct {
	AttemptID  string `gorm:"type:varchar(128);primaryKey"`
	RequestID  string `gorm:"type:varchar(128);not null;index"`
	PluginID   int64  `gorm:"not null;index"`
	TaskID     string `gorm:"type:varchar(191);index"`
	Key        string `gorm:"type:varchar(128);not null"`
	Version    string `gorm:"type:varchar(64);not null"`
	SourceHash string `gorm:"type:varchar(64);not null"`
	CreatedAt  int64  `gorm:"not null"`
}

var ErrTaskPluginInUse = errors.New("plugin version is active or retained by an execution")

// New sends must agree with the authoritative publication, even when another
// instance has not yet refreshed its local routing generation.
func ValidateTaskPluginAdmissionWithTx(tx *gorm.DB, ref TaskPluginRef) error {
	if !constant.TaskPluginEnabled {
		return errors.New("task plugins are disabled")
	}
	if err := lockTaskPluginNamespace(tx, ref.Key); err != nil {
		return err
	}
	var version TaskPlugin
	if err := lockForUpdate(tx).Where(&TaskPlugin{Key: ref.Key, Version: ref.Version}).First(&version).Error; err != nil {
		return err
	}
	if !version.Factory {
		if !version.Active || !version.Enabled {
			return errors.New("plugin publication changed before sending")
		}
		return nil
	}
	if setting.IsTaskPluginFactoryDisabled(ref.Key) {
		return errors.New("factory plugin is disabled")
	}
	var overrides int64
	if err := tx.Model(&TaskPlugin{}).Where(&TaskPlugin{Key: ref.Key, Active: true}).Where("factory = ?", false).Count(&overrides).Error; err != nil {
		return err
	}
	if overrides != 0 {
		return errors.New("factory plugin was replaced before sending")
	}
	return nil
}

func lockTaskPluginNamespace(tx *gorm.DB, key string) error {
	if key == "" {
		return errors.New("plugin key is required")
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&TaskPluginNamespace{Key: key}).Error; err != nil {
		return err
	}
	return lockForUpdate(tx).First(&TaskPluginNamespace{}, &TaskPluginNamespace{Key: key}).Error
}

func PinTaskPluginAttempt(ctx context.Context, requestID, attemptID string, ref TaskPluginRef) error {
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		return PinTaskPluginAttemptWithTx(tx, requestID, attemptID, ref)
	})
}

func PinTaskPluginAttemptWithTx(tx *gorm.DB, requestID, attemptID string, ref TaskPluginRef) error {
	if requestID == "" || attemptID == "" || ref.Key == "" || ref.Version == "" || len(ref.SourceHash) != 64 {
		return errors.New("complete immutable plugin reference is required")
	}
	if err := lockTaskPluginNamespace(tx, ref.Key); err != nil {
		return err
	}
	var version TaskPlugin
	if err := lockForUpdate(tx).Where(&TaskPlugin{Key: ref.Key, Version: ref.Version}).First(&version).Error; err != nil {
		return err
	}
	if version.SourceHash != ref.SourceHash || fmt.Sprintf("%x", sha256.Sum256([]byte(version.Source))) != ref.SourceHash {
		return errors.New("plugin source integrity check failed")
	}
	var existing TaskPluginExecution
	err := tx.Where("attempt_id = ?", attemptID).First(&existing).Error
	if err == nil {
		if existing.RequestID != requestID || existing.PluginID != version.Id || existing.SourceHash != ref.SourceHash {
			return ErrBillingEventConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&TaskPluginExecution{AttemptID: attemptID, RequestID: requestID, PluginID: version.Id,
		Key: ref.Key, Version: ref.Version, SourceHash: ref.SourceHash, CreatedAt: common.GetTimestamp()}).Error
}

func AttachTaskPluginExecution(tx *gorm.DB, task *Task) error {
	ref := task.PrivateData.PluginRef
	if ref == nil {
		return nil
	}
	if task.PrivateData.PluginAttemptID == "" {
		return errors.New("plugin task has no pinned attempt")
	}
	var execution TaskPluginExecution
	if err := lockForUpdate(tx).Where("attempt_id = ?", task.PrivateData.PluginAttemptID).First(&execution).Error; err != nil {
		return err
	}
	if execution.RequestID != task.PrivateData.SubmissionRequestID || execution.Key != ref.Key || execution.Version != ref.Version || execution.SourceHash != ref.SourceHash ||
		(execution.TaskID != "" && execution.TaskID != task.TaskID) {
		return ErrBillingEventConflict
	}
	return tx.Model(&execution).Update("task_id", task.TaskID).Error
}

func GetTaskPluginExact(ref TaskPluginRef) (*TaskPlugin, error) {
	if ref.Key == "" || ref.Version == "" || len(ref.SourceHash) != 64 {
		return nil, errors.New("immutable plugin reference is missing")
	}
	plugin, err := GetTaskPluginVersion(ref.Key, ref.Version)
	if err != nil {
		return nil, err
	}
	if plugin.SourceHash != ref.SourceHash || fmt.Sprintf("%x", sha256.Sum256([]byte(plugin.Source))) != ref.SourceHash {
		return nil, errors.New("plugin source integrity check failed")
	}
	return plugin, nil
}
