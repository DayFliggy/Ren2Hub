package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func taskConsumptionParams(c *gin.Context, info *relaycommon.RelayInfo) model.RecordConsumeLogParams {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if otherRatios := info.PriceData.OtherRatios(); len(otherRatios) > 0 {
			var contents []string
			for key, ra := range otherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["channel_ratio"] = info.PriceData.ChannelRatio
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	attachQuotaSaturation(c, info, other)
	return model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
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
	task.PrivateData.SubmissionRequestID = info.RequestId
	if err := model.PersistAcceptedTask(task, info.RequestId, consumption); err != nil {
		return err
	}
	err = SettleBilling(c, info, task.Quota)
	if err == nil {
		err = model.ApplyBillingRecoveryConsumption(info.RequestId)
	}
	info.BillingSettlementError = err
	if err != nil {
		logger.LogError(c, fmt.Sprintf("accepted task accounting remains pending (request_id=%s, task_id=%s): %v", info.RequestId, task.TaskID, err))
	}
	return nil
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["channel_ratio"] = bc.GroupRatio
		if priceData := taskBillingContextPriceData(bc); priceData != nil {
			for k, v := range priceData.OtherRatios() {
				other[k] = v
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

func taskBillingContextPriceData(bc *model.TaskBillingContext) *types.PriceData {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return nil
	}
	priceData := &types.PriceData{}
	if !priceData.ReplaceOtherRatios(bc.OtherRatios) {
		return nil
	}
	return priceData
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
func completeTaskBilling(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, actualQuota int, reason string, clamps ...*common.QuotaClamp) (bool, error) {
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
	delta := actualQuota - task.Quota
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	consumption := model.BillingConsumption{
		Log: model.Log{UserId: task.UserId, Username: username, CreatedAt: common.GetTimestamp(),
			TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId, ModelName: taskModelName(task),
			Group: task.Group, Content: reason, Other: string(encoded)},
		CountUsage: true, LogEnabled: delta < 0 || common.LogConsumeEnabled,
		ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled, NodeName: nodeName,
	}
	won, err := model.PersistTaskCompletion(ctx, task, fromStatus, actualQuota, consumption)
	if err != nil || !won {
		return won, err
	}
	_, err = recoverOneBillingRecovery(ctx, model.TaskBillingRequestID(task.ID))
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("task completion accounting remains pending (task_id=%s): %v", task.TaskID, err))
		return true, err
	}
	task.Quota = actualQuota
	return true, nil
}

// taskCompletionQuota uses only the accepted price snapshot; zero from the
// adaptor means no adjustment under the existing adaptor contract.
func taskCompletionQuota(adaptor TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo) (int, string, *common.QuotaClamp, error) {
	if task.Status == model.TaskStatusFailure {
		return 0, task.FailReason, nil, nil
	}
	billing := task.PrivateData.BillingContext
	if billing != nil && billing.PerCallBilling {
		return task.Quota, "按次计费", nil, nil
	}
	if actualQuota := adaptor.AdjustBillingOnComplete(task, result); actualQuota > 0 {
		return actualQuota, "adaptor计费调整", nil, nil
	}
	if result.TotalTokens <= 0 {
		return task.Quota, "保持提交额度", nil, nil
	}
	if billing == nil {
		return 0, "", nil, fmt.Errorf("task %s has no billing snapshot; token settlement requires reconciliation", task.TaskID)
	}
	if billing.ModelRatio <= 0 {
		return task.Quota, "保持提交额度", nil, nil
	}
	multiplier := 1.0
	if priceData := taskBillingContextPriceData(billing); priceData != nil {
		multiplier = priceData.OtherRatioMultiplier()
	}
	quota, clamp := common.QuotaFromFloatChecked(float64(result.TotalTokens) * billing.ModelRatio * billing.GroupRatio * multiplier)
	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, channelRatio=%.2f, otherMultiplier=%.4f", result.TotalTokens, billing.ModelRatio, billing.GroupRatio, multiplier)
	return quota, reason, clamp, nil
}
