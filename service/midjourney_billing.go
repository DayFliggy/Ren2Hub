package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func FinalizeMidjourneySubmission(c *gin.Context, info *relaycommon.RelayInfo, task *model.Midjourney) error {
	task.TokenId = info.TokenId
	task.BillingChannelId = info.ChannelId
	task.PrivateData.TokenId = info.TokenId
	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.SubscriptionId = info.SubscriptionId
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.SubmissionRequestID = info.RequestId
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice: info.PriceData.ModelPrice, GroupRatio: info.PriceData.ChannelRatio,
		OriginModelName: info.OriginModelName, PerCallBilling: true,
	}
	params := taskConsumptionParams(c, info)
	params.Quota = task.Quota
	consumption, err := billingConsumptionSnapshot(c, info, params, true)
	if err != nil {
		return err
	}
	task.PrivateData.SubmissionRequestID = info.RequestId
	if err := model.PersistAcceptedMidjourney(task, info.RequestId, consumption); err != nil {
		return err
	}
	err = SettleBilling(c, info, task.Quota)
	if err == nil {
		err = model.ApplyBillingRecoveryConsumption(info.RequestId)
	}
	info.BillingSettlementError = err
	if err != nil {
		logger.LogError(c, fmt.Sprintf("accepted Midjourney accounting remains pending (request_id=%s, row_id=%d): %v", info.RequestId, task.Id, err))
	}
	return nil
}

func CompleteMidjourneyBilling(ctx context.Context, task *model.Midjourney, fromStatus string) (bool, error) {
	tokenID := task.PrivateData.TokenId
	if task.PrivateData.SubmissionRequestID == "" {
		tokenID = task.TokenId
	}
	username, err := model.GetUsernameById(task.UserId, false)
	if err != nil {
		return false, err
	}
	other, err := common.Marshal(map[string]any{"task_id": task.MjId, "reason": task.FailReason, "pre_consumed_quota": task.Quota})
	if err != nil {
		return false, err
	}
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	consumption := model.BillingConsumption{
		Log: model.Log{UserId: task.UserId, Username: username, CreatedAt: common.GetTimestamp(),
			TokenId: tokenID, ChannelId: task.GetBillingChannelId(), ModelName: CovertMjpActionToModelName(task.Action),
			Content: task.FailReason, Other: string(other)},
		CountUsage: true, LogEnabled: true,
		ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled, NodeName: nodeName,
	}
	won, err := model.PersistMidjourneyCompletion(ctx, task, fromStatus, consumption)
	if err != nil || !won {
		return won, err
	}
	_, err = recoverOneBillingRecovery(ctx, model.MidjourneyBillingRequestID(task.Id))
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Midjourney completion accounting remains pending (row_id=%d): %v", task.Id, err))
		return true, err
	}
	if task.Status == "FAILURE" {
		task.Quota = 0
	}
	return true, nil
}
