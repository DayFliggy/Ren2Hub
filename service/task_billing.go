package service

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func taskConsumptionParams(c *gin.Context, info *relaycommon.RelayInfo) model.RecordConsumeLogParams {
	tokenName := c.GetString("token_name")
	logContent := []*common.Message{common.NewMessage("Action {{action}}", map[string]any{"action": info.Action})}
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
	consumption, err := prepareTaskSubmission(c, info, task)
	if err != nil {
		return err
	}
	if err := model.PersistAcceptedTask(task, info.RequestId, consumption); err != nil {
		return err
	}
	if session, ok := info.Billing.(*BillingSession); ok {
		session.stopRecoveryHeartbeat()
	}
	return nil
}

func prepareTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) (model.BillingConsumption, error) {
	consumption, err := billingConsumptionSnapshot(c, info, taskConsumptionParams(c, info), true)
	if err != nil {
		return model.BillingConsumption{}, err
	}
	if task.PrivateData.BillingContext == nil {
		task.PrivateData.BillingContext = &model.TaskBillingContext{}
	}
	task.PrivateData.BillingContext.AttemptID = info.BillingAttemptID
	task.PrivateData.BillingContext.Quote = info.PriceData.Quote
	task.PrivateData.BillingContext.Usage = info.PriceData.Usage
	task.PrivateData.SubmissionRequestID = info.RequestId
	return consumption, nil
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := map[string]interface{}{"is_task": true}
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
		if task.Status == model.TaskStatusSuccess && bc.Usage != nil && billing.IsFrozenEstimateSource(bc.Usage.Source) {
			other["admin_info"] = map[string]any{
				"usage_billing_path":      billing.SettlementFrozenEstimate,
				"billing_fallback_reason": billingFallbackUnpriced,
			}
		}
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
// poll or the timeout sweep finishes it. settlement describes how a completed
// task was priced; a failed task passes nil and its log carries the fail reason.
func completeTaskBilling(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, actualQuota common.Quota, settlement *common.Message, clamps ...*common.QuotaClamp) (bool, error) {
	consumption, err := taskCompletionConsumption(task, actualQuota, settlement, clamps...)
	if err != nil {
		return false, err
	}
	won, err := model.PersistTaskCompletion(ctx, task, fromStatus, actualQuota, consumption)
	if err != nil || !won {
		return won, err
	}
	return true, deliverTaskCompletion(ctx, task, actualQuota)
}

func taskCompletionConsumption(task *model.Task, actualQuota common.Quota, settlement *common.Message, clamps ...*common.QuotaClamp) (model.BillingConsumption, error) {
	other := taskBillingOther(task)
	content := task.FailReason
	if settlement != nil {
		content = model.SetLogContent(other, []*common.Message{settlement})
	}
	other["task_id"], other["reason"] = task.TaskID, content
	other["pre_consumed_quota"], other["actual_quota"] = task.Quota, actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	encoded, err := common.Marshal(other)
	if err != nil {
		return model.BillingConsumption{}, err
	}
	username, err := model.GetUsernameById(task.UserId, false)
	if err != nil {
		return model.BillingConsumption{}, err
	}
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	consumption := model.BillingConsumption{
		Log: model.Log{UserId: task.UserId, Username: username, CreatedAt: common.GetTimestamp(),
			TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId, ModelName: taskModelName(task),
			Group: task.Group, Content: content, Other: string(encoded)},
		CountUsage: true, LogEnabled: actualQuota < task.Quota || common.LogConsumeEnabled,
		ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled, NodeName: nodeName,
	}
	if snapshot := task.PrivateData.BillingContext; snapshot != nil {
		consumption.AttemptID = snapshot.AttemptID
		consumption.Usage, consumption.Charge = snapshot.Usage, snapshot.Charge
	}
	return consumption, nil
}

func deliverTaskCompletion(ctx context.Context, task *model.Task, actualQuota common.Quota) error {
	if err := model.DeliverTaskResultMetrics(ctx, task.ID); err != nil {
		logger.LogError(ctx, fmt.Sprintf("task result metrics remain pending (task_id=%s): %v", task.TaskID, err))
	}
	_, err := recoverOneBillingRecovery(ctx, task.PrivateData.SubmissionRequestID)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("task completion accounting remains pending (task_id=%s): %v", task.TaskID, err))
		return err
	}
	task.Quota = actualQuota
	return nil
}

// taskCompletionQuota never accepts an adaptor-computed monetary value. A
// completed task consumes its accepted quote and its usage facts: the frozen
// submit facts overwritten key by key by the provider's completion facts.
// When those facts cannot be priced it consumes its submission estimate.
func taskCompletionQuota(_ TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) (common.Quota, *common.Message, *common.QuotaClamp, error) {
	if task.Status == model.TaskStatusFailure {
		return 0, nil, nil, nil
	}
	var submitted map[string]any
	if snapshot := task.PrivateData.BillingContext; snapshot != nil && snapshot.Quote != nil && snapshot.Quote.PriceVersion.TaskUsage &&
		result != nil && result.UsageError == "" {
		facts, err := model.FrozenTaskUsageFacts(task.PrivateData.SubmissionRequestID, snapshot.AttemptID)
		// Facts that fail verification keep the task for the next attempt
		// instead of settling at the estimate derived from those same facts.
		// An attempt that never persisted an estimate merges from nothing.
		if err != nil && !errors.Is(err, model.ErrBillingEstimateMissing) {
			return 0, nil, nil, err
		}
		submitted = facts
	}
	quota, err := actualTaskCompletionQuota(task, result, submitted)
	if err == nil {
		return quota, common.NewMessage("Settled at the frozen quote"), nil, nil
	}
	quota, estimateErr := frozenTaskCompletionQuota(task)
	if estimateErr != nil {
		return 0, nil, nil, errors.Join(err, estimateErr)
	}
	return quota, common.NewMessage("Completion usage could not be priced, settled at the submission estimate"), nil, nil
}

func actualTaskCompletionQuota(task *model.Task, result *relaycommon.TaskInfo, submitted map[string]any) (common.Quota, error) {
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
		// As upstream EvaluateTaskCompletionUsage: the frozen submit facts
		// are the base and every reported completion fact, explicit zero and
		// false included, replaces its key. An empty completion report
		// therefore prices exactly the submit facts the estimate priced.
		merged := make(map[string]any, len(submitted)+len(result.UsageFacts))
		maps.Copy(merged, submitted)
		maps.Copy(merged, result.UsageFacts)
		usage.EstimatedFacts, usage.ActualFacts = submitted, result.UsageFacts
		var err error
		input.Usage, err = TaskExpressionUsage(merged, version.UsageSchema)
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

// settleTaskResult prices a provider terminal result already applied to task
// and commits it with its accounting intent. When the attempt's billing
// evidence cannot be verified, the result is held on the still non-terminal
// task instead: the reservation stays held and neither the frozen estimate nor
// the timeout sweep ends the task.
func settleTaskResult(ctx context.Context, task *model.Task, from model.TaskStatus, result *relaycommon.TaskInfo) error {
	quota, reason, clamp, err := taskCompletionQuota(nil, task, result)
	won := false
	if err == nil {
		won, err = completeTaskBilling(ctx, task, from, quota, reason, clamp)
	}
	if won || task.Status != model.TaskStatusSuccess || !model.IsBillingEvidenceUnverifiable(err) {
		return err
	}
	newly, holdErr := model.PersistTaskSettlementBlock(ctx, task, from, heldTaskResult(task))
	if holdErr != nil {
		return errors.Join(err, holdErr)
	}
	if newly {
		logger.LogError(ctx, fmt.Sprintf("task settlement blocked, result held (task_id=%s, reason=%s): %v", task.TaskID, model.TaskSettlementBlockedEvidence, err))
	}
	return nil
}

func heldTaskResult(task *model.Task) model.TaskBlockedResult {
	return model.TaskBlockedResult{Status: task.Status, Progress: task.Progress, FailReason: task.FailReason,
		FinishTime: task.FinishTime, ResultURL: task.PrivateData.ResultURL, Data: task.Data}
}

// CompleteImmediateTask uses exactly the same durable terminal accounting as
// polling, including the submission-estimate fallback and the blocked hold.
func CompleteImmediateTask(ctx context.Context, task *model.Task, result *relaycommon.TaskInfo) error {
	from := task.Status
	if err := applyImmediateTaskResult(task, result); err != nil {
		return err
	}
	return settleTaskResult(ctx, task, from, result)
}

// FinalizeImmediateTaskSubmission atomically saves the accepted terminal job,
// its completion usage and the verified accounting intent. The bool reports
// durable ownership even when delivery subsequently fails. A result whose
// settlement is blocked is saved as an accepted task holding it and reported
// as model.ErrTaskSettlementBlocked; it must not be delivered inline.
func FinalizeImmediateTaskSubmission(ctx context.Context, c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, result *relaycommon.TaskInfo) (bool, error) {
	submission, err := prepareTaskSubmission(c, info, task)
	if err != nil {
		return false, err
	}
	prior := *task
	if err := applyImmediateTaskResult(task, result); err != nil {
		return false, err
	}
	quota, reason, clamp, err := taskCompletionQuota(nil, task, result)
	committed := false
	if err == nil {
		var completion model.BillingConsumption
		completion, err = taskCompletionConsumption(task, quota, reason, clamp)
		if err == nil {
			committed, err = model.PersistImmediateTask(ctx, task, prior.Status, quota, submission, completion)
		}
	}
	if !committed && task.Status == model.TaskStatusSuccess && model.IsBillingEvidenceUnverifiable(err) {
		return holdBlockedImmediateTask(ctx, info, task, prior, submission, err)
	}
	if err != nil || !committed {
		return false, err
	}
	if session, ok := info.Billing.(*BillingSession); ok {
		session.stopRecoveryHeartbeat()
	}
	return true, deliverTaskCompletion(ctx, task, quota)
}

// holdBlockedImmediateTask saves an immediate result whose settlement is
// blocked as an accepted task in its pre-result state that holds the result.
// Nothing was delivered inline, so a route that discards results after inline
// delivery keeps this one for retrieval once settlement succeeds.
func holdBlockedImmediateTask(ctx context.Context, info *relaycommon.RelayInfo, task *model.Task, prior model.Task, submission model.BillingConsumption, cause error) (bool, error) {
	held := heldTaskResult(task)
	task.Status, task.Progress, task.FinishTime, task.FailReason = prior.Status, prior.Progress, prior.FinishTime, prior.FailReason
	task.Data, task.PrivateData.ResultURL, task.PrivateData.ResultDiscarded = nil, "", false
	task.PrivateData.BillingContext.Usage, task.PrivateData.BillingContext.Charge = info.PriceData.Usage, nil
	if err := model.PersistBlockedImmediateTask(ctx, task, submission, held); err != nil {
		return false, errors.Join(cause, err)
	}
	if session, ok := info.Billing.(*BillingSession); ok {
		session.stopRecoveryHeartbeat()
	}
	logger.LogError(ctx, fmt.Sprintf("task settlement blocked, result held (task_id=%s, reason=%s): %v", task.TaskID, model.TaskSettlementBlockedEvidence, cause))
	return true, model.ErrTaskSettlementBlocked
}

// ApplyRunningImmediateResult stores the provider state of an immediate
// result that is not terminal yet, as upstream does; the task is then
// accepted asynchronously and polled. An unrecognized status keeps the
// initial state for polling to resolve.
func ApplyRunningImmediateResult(task *model.Task, result *relaycommon.TaskInfo) {
	if result == nil {
		return
	}
	switch model.TaskStatus(result.Status) {
	case model.TaskStatusSubmitted:
		task.Status, task.Progress = model.TaskStatusSubmitted, taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Status, task.Progress = model.TaskStatusQueued, taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Status, task.Progress = model.TaskStatusInProgress, taskcommon.ProgressInProgress
	}
	if result.PluginState != nil {
		task.PrivateData.PluginState = result.PluginState
	}
}

func applyImmediateTaskResult(task *model.Task, result *relaycommon.TaskInfo) error {
	if result == nil || (result.Status != string(model.TaskStatusSuccess) && result.Status != string(model.TaskStatusFailure)) {
		return fmt.Errorf("immediate task requires an explicit terminal status")
	}
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
	return nil
}
