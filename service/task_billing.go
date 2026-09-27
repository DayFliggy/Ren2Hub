package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func taskConsumptionParams(c *gin.Context, info *relaycommon.RelayInfo) model.RecordConsumeLogParams {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	other := map[string]interface{}{"is_task": true, "request_path": c.Request.URL.Path}
	InjectBillingPriceInfo(other, info)
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	attachQuotaSaturation(c, info, other)
	return model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     common.Quota(info.PriceData.Quota),
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	}
}

// FinalizeTaskSubmission returns an error only when the accepted job could not
// be saved. Accounting errors after that point remain in durable recovery.
func FinalizeTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) error {
	consumption, err := billingConsumptionSnapshot(c, info, taskConsumptionParams(c, info), true)
	if err != nil {
		return err
	}
	if task.PrivateData.BillingContext == nil {
		task.PrivateData.BillingContext = &model.TaskBillingContext{}
	}
	task.PrivateData.BillingContext.Quote = info.PriceData.Quote
	task.PrivateData.BillingContext.Usage = info.PriceData.Usage
	task.PrivateData.SubmissionRequestID = info.RequestId
	if err := model.PersistAcceptedTask(task, info.RequestId, consumption); err != nil {
		return err
	}
	if session, ok := info.Billing.(*BillingSession); ok {
		session.stopRecoveryHeartbeat()
	}
	return nil
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		if bc.Quote != nil {
			other["price_scope"] = bc.Quote.PriceVersion.ScopeKind
			other["price_channel_id"] = bc.Quote.PriceVersion.ChannelID
			other["price_version_id"] = bc.Quote.PriceVersion.ID
			other["quote_hash"] = bc.Quote.Hash
			other["price_version_hash"] = bc.Quote.PriceVersion.Hash
			other["channel_multiplier"] = bc.Quote.ChannelRatio
			other["billing_mode"] = bc.Quote.PriceVersion.Mode
		}
		other["billing_usage"] = bc.Usage
		other["charge_result"] = bc.Charge
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// completeTaskBilling keeps provider completion separate from recoverable
// accounting. Once the intent commits, polling must never replay the job.
func completeTaskBilling(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, actualQuota common.Quota, reason string, clamps ...*common.QuotaClamp) (won bool, completionErr error) {
	defer func() {
		if completionErr == nil || won {
			return
		}
		// A failed accounting preparation cannot erase confirmed provider
		// completion. Preserve it with the reservation for reconciliation.
		holdErr := model.HoldTaskBilling(ctx, task, fromStatus, "task_terminal_accounting_pending")
		completionErr = errors.Join(completionErr, holdErr)
		if holdErr == nil {
			if err := model.DeliverTaskResultMetrics(ctx, task.ID); err != nil {
				logger.LogError(ctx, fmt.Sprintf("task result metrics remain pending (task_id=%s): %v", task.TaskID, err))
			}
		}
	}()
	other := taskBillingOther(task)
	other["task_id"], other["reason"] = task.TaskID, reason
	other["pre_consumed_quota"], other["actual_quota"] = task.Quota, actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	encoded, err := common.Marshal(other)
	if err != nil {
		return false, err
	}
	username, err := model.GetUsernameById(task.UserId, false)
	if err != nil {
		return false, err
	}
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	consumption := model.BillingConsumption{
		Log: model.Log{UserId: task.UserId, Username: username, CreatedAt: common.GetTimestamp(),
			TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId, ModelName: taskModelName(task),
			Group: task.Group, Content: reason, Other: string(encoded)},
		CountUsage: true, LogEnabled: actualQuota < task.Quota || common.LogConsumeEnabled,
		ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled, NodeName: nodeName,
	}
	if snapshot := task.PrivateData.BillingContext; snapshot != nil {
		consumption.Usage, consumption.Charge = snapshot.Usage, snapshot.Charge
	}
	won, err = model.PersistTaskCompletion(ctx, task, fromStatus, actualQuota, consumption)
	if err != nil || !won {
		return won, err
	}
	if err := model.DeliverTaskResultMetrics(ctx, task.ID); err != nil {
		logger.LogError(ctx, fmt.Sprintf("task result metrics remain pending (task_id=%s): %v", task.TaskID, err))
	}
	_, err = recoverOneBillingRecovery(ctx, task.PrivateData.SubmissionRequestID)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("task completion accounting remains pending (task_id=%s): %v", task.TaskID, err))
		return true, err
	}
	task.Quota = actualQuota
	return true, nil
}

// taskCompletionQuota never accepts an adaptor-computed monetary value. A
// completed task consumes its accepted quote and the provider's usage facts.
func taskCompletionQuota(_ TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) (common.Quota, string, *common.QuotaClamp, error) {
	if task.Status == model.TaskStatusFailure {
		return 0, task.FailReason, nil, nil
	}
	snapshot := task.PrivateData.BillingContext
	if snapshot == nil || snapshot.Quote == nil || snapshot.Usage == nil {
		return 0, "", nil, fmt.Errorf("task %s has no frozen billing quote; reconciliation required", task.TaskID)
	}
	usage := *snapshot.Usage
	usage.Quantities = make(map[string]string, len(snapshot.Usage.Quantities))
	for name, count := range snapshot.Usage.Quantities {
		usage.Quantities[name] = count
	}
	usage.Source = "task_completed"
	usage.Complete = true
	usage.Estimated = false
	if task.PrivateData.PluginRef != nil {
		if result == nil || result.UsageError != "" {
			return 0, "", nil, fmt.Errorf("plugin completion usage unavailable; reconciliation required")
		}
		usage.Quantities = nil
		if snapshot.Quote.PriceVersion.UsageSchema == nil {
			usage.Quantities = map[string]string{"calls": "1"}
		}
		usage.EstimatedFacts = nil
		usage.ActualFacts = nil
		// A schema-free fixed call or free price has no provider fact inputs.
		// Preserve the provider's evidence in CompletionUsage instead.
		if snapshot.Quote.PriceVersion.UsageSchema != nil {
			usage.ActualFacts = result.UsageFacts
		}
	}
	if result != nil && (result.TotalTokens < 0 || result.CompletionTokens < 0 || result.CompletionTokens > result.TotalTokens) {
		return 0, "", nil, fmt.Errorf("invalid task token usage")
	}
	if result != nil && result.TotalTokens > 0 {
		usage.OutputTokens = int64(result.CompletionTokens)
		usage.InputTokens = int64(result.TotalTokens - result.CompletionTokens)
		usage.ContextTokens = usage.InputTokens
	} else {
		usesTokens := false
		if snapshot.Quote.PriceVersion.UsageSchema == nil {
			for _, price := range snapshot.Quote.PriceVersion.Prices {
				usesTokens = usesTokens || price.Unit == "per_million_tokens"
			}
			if snapshot.Quote.PriceVersion.Mode == billing.ModeExpression {
				used := billingexpr.UsedVars(snapshot.Quote.PriceVersion.Expression)
				for _, name := range []string{"p", "c", "reasoning", "len", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao"} {
					usesTokens = usesTokens || used[name]
				}
			}
		}
		if usesTokens {
			return 0, "", nil, fmt.Errorf("task token usage unavailable; reconciliation required")
		}
		usage.InputTokens, usage.OutputTokens, usage.ContextTokens = 0, 0, 0
	}
	charge, err := billing.Calculate(snapshot.Quote, usage)
	if err != nil {
		return 0, "", nil, err
	}
	snapshot.Usage = &usage
	snapshot.Charge = charge
	return charge.FinalQuota, "按冻结报价结算", nil, nil
}

// CompleteImmediateTask uses exactly the same durable terminal accounting as
// polling. An invalid completion retains its reservation for reconciliation.
func CompleteImmediateTask(ctx context.Context, task *model.Task, result *relaycommon.TaskInfo) error {
	if result == nil || (result.Status != string(model.TaskStatusSuccess) && result.Status != string(model.TaskStatusFailure)) {
		return fmt.Errorf("immediate task requires an explicit terminal status")
	}
	from := task.Status
	task.Status = model.TaskStatus(result.Status)
	task.Progress = "100%"
	task.FinishTime = common.GetTimestamp()
	task.FailReason = result.Reason
	task.PrivateData.ResultURL = result.Url
	task.PrivateData.CompletionUsage = &model.TaskCompletionUsage{CompletionTokens: result.CompletionTokens, TotalTokens: result.TotalTokens, Facts: result.UsageFacts, Error: result.UsageError}
	if result.FinishTime > 0 {
		task.FinishTime = result.FinishTime
	}
	if result.PluginState != nil {
		task.PrivateData.PluginState = result.PluginState
	}
	quota, reason, clamp, err := taskCompletionQuota(nil, task, result)
	if err != nil {
		if holdErr := model.HoldTaskBilling(ctx, task, from, "task_final_usage_invalid"); holdErr != nil {
			return fmt.Errorf("%w; retain reservation: %v", err, holdErr)
		}
		if metricErr := model.DeliverTaskResultMetrics(ctx, task.ID); metricErr != nil {
			logger.LogError(ctx, fmt.Sprintf("task result metrics remain pending (task_id=%s): %v", task.TaskID, metricErr))
		}
		return err
	}
	_, err = completeTaskBilling(ctx, task, from, quota, reason, clamp)
	return err
}
