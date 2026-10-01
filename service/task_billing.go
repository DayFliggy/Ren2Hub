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
	task.PrivateData.BillingContext.AttemptID = info.BillingAttemptID
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
		other["billing_usage"] = publicBillingUsage(bc.Usage)
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
// A failed completion transaction leaves the task non-terminal, so the next
// poll or the timeout sweep finishes it.
func completeTaskBilling(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, actualQuota common.Quota, reason string, clamps ...*common.QuotaClamp) (bool, error) {
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
		consumption.AttemptID = snapshot.AttemptID
		consumption.Usage, consumption.Charge = snapshot.Usage, snapshot.Charge
	}
	won, err := model.PersistTaskCompletion(ctx, task, fromStatus, actualQuota, consumption)
	if err != nil || !won {
		return won, err
	}
	if task.Status == model.TaskStatusSuccess {
		billing.RecordSettlement(billing.SettlementBasis(consumption.Usage))
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
// completed task consumes its accepted quote and the provider's usage facts;
// when those facts cannot be priced it consumes its submission estimate.
func taskCompletionQuota(_ TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) (common.Quota, string, *common.QuotaClamp, error) {
	if task.Status == model.TaskStatusFailure {
		return 0, task.FailReason, nil, nil
	}
	quota, err := actualTaskCompletionQuota(task, result)
	if err == nil {
		return quota, "按冻结报价结算", nil, nil
	}
	quota, estimateErr := frozenTaskCompletionQuota(task)
	if estimateErr != nil {
		return 0, "", nil, errors.Join(err, estimateErr)
	}
	return quota, "完成用量无法计价，按提交时估算结算", nil, nil
}

func actualTaskCompletionQuota(task *model.Task, result *relaycommon.TaskInfo) (common.Quota, error) {
	snapshot := task.PrivateData.BillingContext
	if snapshot == nil || snapshot.Quote == nil || snapshot.Usage == nil {
		return 0, fmt.Errorf("task %s has no frozen billing quote", task.TaskID)
	}
	usage := billing.Usage{Source: "task_completed", Terminal: "completed", Complete: true, Quantities: map[string]string{"calls": "1"}}
	version := snapshot.Quote.PriceVersion
	if result != nil && result.UsageError != "" {
		return 0, fmt.Errorf("plugin completion usage unavailable")
	}
	if result != nil && (result.TotalTokens < 0 || result.CompletionTokens < 0 || result.CompletionTokens > result.TotalTokens) {
		return 0, fmt.Errorf("invalid task token usage")
	}
	if version.NeedsTokenUsage() {
		if result == nil || result.TotalTokens == 0 {
			return 0, fmt.Errorf("task token usage unavailable")
		}
		usage.OutputTokens = int64(result.CompletionTokens)
		usage.InputTokens = int64(result.TotalTokens - result.CompletionTokens)
		usage.ContextTokens = usage.InputTokens
	}
	input := billingexpr.RequestInput{}
	if version.TaskUsage {
		if result == nil {
			return 0, fmt.Errorf("plugin completion usage unavailable")
		}
		usage.ActualFacts = result.UsageFacts
		var err error
		input.Usage, err = TaskExpressionUsage(result.UsageFacts, version.UsageSchema)
		if err != nil {
			return 0, err
		}
	}
	var charge *billing.ChargeResult
	var err error
	if snapshot.Usage.Confirmed() && snapshot.Charge != nil {
		usage, charge, err = replayIdenticalBillingUsage(snapshot.Quote, *snapshot.Usage, usage, task.PrivateData.SubmissionRequestID, snapshot.AttemptID)
	} else {
		usage, charge, err = model.EvaluateBilling(snapshot.Quote, usage, input, task.PrivateData.SubmissionRequestID, snapshot.AttemptID, "settlement")
	}
	if err != nil {
		return 0, err
	}
	snapshot.Usage = &usage
	snapshot.Charge = charge
	return charge.FinalQuota, nil
}

func frozenTaskCompletionQuota(task *model.Task) (common.Quota, error) {
	snapshot := task.PrivateData.BillingContext
	if snapshot == nil {
		return 0, model.ErrBillingEstimateMissing
	}
	usage, charge, err := model.FrozenTaskBillingEstimate(task.PrivateData.SubmissionRequestID, snapshot.AttemptID)
	if err != nil {
		return 0, err
	}
	snapshot.Usage, snapshot.Charge = usage, charge
	return charge.FinalQuota, nil
}

// CompleteImmediateTask uses exactly the same durable terminal accounting as
// polling, including the submission-estimate fallback.
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
		return err
	}
	_, err = completeTaskBilling(ctx, task, from, quota, reason, clamp)
	return err
}
