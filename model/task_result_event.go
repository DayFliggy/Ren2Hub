package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"reflect"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"gorm.io/gorm"
)

// TaskResultEvent is the unique provider outcome and a durable metrics outbox.
// Money and consumption statistics continue through BillingRecovery separately.
type TaskResultEvent struct {
	TaskID          int64      `gorm:"primaryKey;autoIncrement:false"`
	RequestID       string     `gorm:"type:varchar(128);not null;index"`
	Status          TaskStatus `gorm:"type:varchar(20);not null"`
	ModelName       string     `gorm:"type:varchar(128);not null"`
	Group           string     `gorm:"type:varchar(64);not null"`
	PluginKey       string     `gorm:"type:varchar(128)"`
	PluginVersion   string     `gorm:"type:varchar(64)"`
	PluginHash      string     `gorm:"type:varchar(64)"`
	ResultHash      string     `gorm:"type:varchar(64);not null"`
	UsageSnapshot   LongText
	SubmitTime      int64  `gorm:"not null"`
	StartTime       int64  `gorm:"not null"`
	FinishTime      int64  `gorm:"not null"`
	ObservedAt      int64  `gorm:"not null"`
	BucketTs        int64  `gorm:"not null"`
	MetricsState    string `gorm:"type:varchar(16);not null;index"`
	MetricsPostedAt int64  `gorm:"not null"`
}

func validateTaskCompletionIdentity(current, task *Task) error {
	if current.TaskID != task.TaskID || current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.Platform != task.Platform ||
		current.PrivateData.SubmissionRequestID != task.PrivateData.SubmissionRequestID ||
		current.PrivateData.TokenId != task.PrivateData.TokenId || current.PrivateData.UpstreamTaskID != task.PrivateData.UpstreamTaskID ||
		current.PrivateData.BillingSource != task.PrivateData.BillingSource || current.PrivateData.CredentialVersion != task.PrivateData.CredentialVersion ||
		current.PrivateData.CredentialID != task.PrivateData.CredentialID || current.PrivateData.EndpointID != task.PrivateData.EndpointID ||
		current.PrivateData.PluginAttemptID != task.PrivateData.PluginAttemptID ||
		!reflect.DeepEqual(current.PrivateData.PluginRef, task.PrivateData.PluginRef) ||
		!reflect.DeepEqual(current.PrivateData.Execution, task.PrivateData.Execution) || current.Properties != task.Properties {
		return ErrBillingRecoveryStateConflict
	}
	if current.PrivateData.BillingContext != nil && current.PrivateData.BillingContext.Quote != nil {
		if task.PrivateData.BillingContext == nil || task.PrivateData.BillingContext.Quote == nil ||
			current.PrivateData.BillingContext.Quote.Hash != task.PrivateData.BillingContext.Quote.Hash {
			return ErrBillingRecoveryStateConflict
		}
	}
	return nil
}

func recordTaskResultEvent(tx *gorm.DB, task *Task) error {
	for _, timestamp := range []int64{task.SubmitTime, task.StartTime, task.FinishTime} {
		if timestamp < 0 || timestamp > math.MaxInt64/1000 {
			return fmt.Errorf("task result timestamp is outside the supported range")
		}
	}
	result, err := common.Marshal(struct {
		Data  any    `json:"data"`
		State any    `json:"state"`
		URL   string `json:"url"`
	}{task.Data, task.PrivateData.PluginState, task.PrivateData.ResultURL})
	if err != nil {
		return err
	}
	usage, err := common.Marshal(task.PrivateData.CompletionUsage)
	if err != nil {
		return err
	}
	event := TaskResultEvent{TaskID: task.ID, RequestID: task.PrivateData.SubmissionRequestID, Status: task.Status,
		ModelName: task.Properties.OriginModelName, Group: task.Group, SubmitTime: task.SubmitTime, StartTime: task.StartTime,
		FinishTime: task.FinishTime, ObservedAt: common.GetTimestamp(), ResultHash: fmt.Sprintf("%x", sha256.Sum256(result)),
		UsageSnapshot: LongText(usage), MetricsState: BillingRecoveryStatePending}
	if event.Group == "" {
		event.Group = "default"
	}
	if ref := task.PrivateData.PluginRef; ref != nil {
		event.PluginKey, event.PluginVersion, event.PluginHash = ref.Key, ref.Version, ref.SourceHash
	}
	if !perf_metrics_setting.GetSetting().Enabled || event.ModelName == "" {
		event.MetricsState = "disabled"
	}
	// Keep the provider's timestamp. Observation time is only the fallback when
	// that timestamp was absent, never the timestamp of a later billing retry.
	at := event.FinishTime
	if at <= 0 {
		at = event.ObservedAt
	}
	seconds := perf_metrics_setting.GetBucketSeconds()
	event.BucketTs = at / seconds * seconds
	return tx.Create(&event).Error
}

// DeliverTaskResultMetrics commits the projection and delivery marker together.
// A rollback or restart can replay the outbox without double-counting a result.
func DeliverTaskResultMetrics(ctx context.Context, taskID int64) error {
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		var event TaskResultEvent
		if err := lockForUpdate(tx).First(&event, taskID).Error; err != nil {
			return err
		}
		if event.MetricsState != BillingRecoveryStatePending {
			return nil
		}
		metric := PerfMetric{ModelName: event.ModelName, Group: event.Group, BucketTs: event.BucketTs, RequestCount: 1}
		if event.Status == TaskStatusSuccess {
			metric.SuccessCount = 1
		}
		if event.SubmitTime > 0 && event.FinishTime >= event.SubmitTime {
			metric.TotalLatencyMs = (event.FinishTime - event.SubmitTime) * 1000
		}
		if event.StartTime > 0 && event.FinishTime >= event.StartTime {
			metric.GenerationMs = (event.FinishTime - event.StartTime) * 1000
		} else {
			metric.GenerationMs = metric.TotalLatencyMs
		}
		var usage *TaskCompletionUsage
		if err := common.UnmarshalJsonStr(string(event.UsageSnapshot), &usage); err != nil {
			return err
		}
		if usage != nil && usage.Error == "" && usage.CompletionTokens >= 0 && usage.CompletionTokens <= usage.TotalTokens && usage.TotalTokens <= common.MaxQuota {
			metric.OutputTokens = int64(usage.CompletionTokens)
		}
		if err := upsertPerfMetricWithTx(tx, &metric); err != nil {
			return err
		}
		return tx.Model(&event).Updates(map[string]any{"metrics_state": BillingRecoveryStateCompleted, "metrics_posted_at": common.GetTimestamp()}).Error
	})
}

func RecoverTaskResultMetrics(ctx context.Context, limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var events []TaskResultEvent
	if err := DB.WithContext(ctx).Select("task_id").Where("metrics_state = ?", BillingRecoveryStatePending).Order("task_id").Limit(limit).Find(&events).Error; err != nil {
		return err
	}
	for _, event := range events {
		if err := DeliverTaskResultMetrics(ctx, event.TaskID); err != nil {
			return err
		}
	}
	return nil
}
