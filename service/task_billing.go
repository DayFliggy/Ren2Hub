package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
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
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, info.PriceData.Quota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["channel_ratio"] = bc.BillingMultiplier
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

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，退还资金与令牌额度，并回减用户和渠道用量。
// 返回资金来源是否已成功退还；失败时保留 quota，供显式重试或人工对账。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) bool {
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	return settleTaskQuota(ctx, task, 0, true, reason, other)
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) {
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	settleTaskQuota(ctx, task, actualQuota, false, reason, other)
}

func settleTaskQuota(ctx context.Context, task *model.Task, finalQuota int, refund bool, reason string, other map[string]any) bool {
	receipt, err := model.ApplyTaskSettlement(ctx, task, finalQuota, refund, model.RecordTaskBillingLogParams{
		UserId: task.UserId, ChannelId: task.ChannelId, TokenId: task.PrivateData.TokenId,
		ModelName: taskModelName(task), Content: reason, Other: other, NodeName: task.PrivateData.NodeName,
	})
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务账务待恢复 task=%s: %s", task.TaskID, err.Error()))
		return false
	}
	if err := model.SyncTaskSettlementCache(ctx, receipt.TaskID); err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务资金已提交，缓存同步待恢复 task=%s: %s", task.TaskID, err.Error()))
	}
	if err := model.DeliverTaskSettlementLog(ctx, receipt.TaskID); err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务资金已提交，日志待恢复 task=%s: %s", task.TaskID, err.Error()))
	}
	return true
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和渠道倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	if totalTokens <= 0 {
		return
	}
	actualQuota, clamp, err := taskQuotaByTokens(task, totalTokens)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("task %s requires billing reconciliation: %s", task.TaskID, err))
		return
	}
	RecalculateTaskQuota(ctx, task, actualQuota, fmt.Sprintf("token重算：tokens=%d", totalTokens), clamp)
}

func taskQuotaByTokens(task *model.Task, totalTokens int) (int, *common.QuotaClamp, error) {
	billing := task.PrivateData.BillingContext
	if billing == nil {
		return 0, nil, errors.New("billing snapshot missing")
	}
	if math.IsNaN(billing.BillingMultiplier) || math.IsInf(billing.BillingMultiplier, 0) || billing.BillingMultiplier < 0 || math.IsNaN(billing.ModelRatio) || math.IsInf(billing.ModelRatio, 0) || billing.ModelRatio < 0 {
		return 0, nil, errors.New("invalid frozen billing multiplier")
	}
	if billing.PerCallBilling || billing.ModelRatio <= 0 {
		return task.Quota, nil, nil
	}
	modelRatio, channelRatio := billing.ModelRatio, billing.BillingMultiplier

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	} else if len(billing.OtherRatios) > 0 {
		return 0, nil, errors.New("invalid frozen billing ratios")
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * channelRatio * otherMultiplier（饱和转换，防止溢出成负数）
	actualQuota, clamp := common.QuotaFromFloatChecked(float64(totalTokens) * modelRatio * channelRatio * otherMultiplier)

	return actualQuota, clamp, nil
}

func taskSettlementInput(task *model.Task, finalQuota int, refund bool, reason string, clamp *common.QuotaClamp) model.TaskSettlementInput {
	other := taskBillingOther(task)
	other["task_id"], other["reason"] = task.TaskID, reason
	attachQuotaSaturationToOther(other, clamp)
	return model.TaskSettlementInput{FinalQuota: finalQuota, Refund: refund, Log: model.RecordTaskBillingLogParams{
		UserId: task.UserId, ChannelId: task.ChannelId, TokenId: task.PrivateData.TokenId,
		ModelName: taskModelName(task), Content: reason, Other: other, NodeName: task.PrivateData.NodeName,
	}}
}

func completeTaskTransition(ctx context.Context, task *model.Task, from model.TaskStatus, input model.TaskSettlementInput) (bool, error) {
	won, err := model.UpdateTaskWithSettlement(ctx, task, from, input)
	if err != nil || !won {
		return won, err
	}
	receipt, err := model.ResumeTaskSettlement(ctx, task.ID)
	if err != nil {
		return true, err
	}
	task.Quota = receipt.FinalQuota
	return true, errors.Join(model.SyncTaskSettlementCache(ctx, task.ID), model.DeliverTaskSettlementLog(ctx, task.ID))
}
