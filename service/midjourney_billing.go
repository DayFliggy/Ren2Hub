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
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.SubmissionRequestID = info.RequestId
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		AttemptID: info.BillingAttemptID, Quote: info.PriceData.Quote, Usage: info.PriceData.Usage, OriginModelName: info.OriginModelName,
	}
	params := taskConsumptionParams(c, info)
	params.Quota = task.Quota
	consumption, err := billingConsumptionSnapshot(c, info, params, true)
	if err != nil {
		return err
	}
	if err := model.PersistAcceptedMidjourney(task, info.RequestId, consumption); err != nil {
		return err
	}
	if session, ok := info.Billing.(*BillingSession); ok {
		session.stopRecoveryHeartbeat()
	}
	return nil
}

func CompleteMidjourneyBilling(ctx context.Context, task *model.Midjourney, fromStatus string) (bool, error) {
	if task == nil || (task.Status != "SUCCESS" && task.Status != "FAILURE") {
		return false, fmt.Errorf("invalid Midjourney completion")
	}
	// Midjourney uses the same immutable quote calculator as other async jobs.
	// The reserved amount is never treated as evidence of the final charge.
	billingTask := &model.Task{TaskID: task.MjId, Status: model.TaskStatus(task.Status), PrivateData: task.PrivateData}
	actualQuota, _, _, err := taskCompletionQuota(nil, billingTask, nil)
	if err != nil {
		return false, err
	}
	username, err := model.GetUsernameById(task.UserId, false)
	if err != nil {
		return false, err
	}
	other := taskBillingOther(billingTask)
	other["task_id"], other["reason"] = task.MjId, task.FailReason
	other["pre_consumed_quota"], other["actual_quota"] = task.Quota, actualQuota
	encoded, err := common.Marshal(other)
	if err != nil {
		return false, err
	}
	nodeName := task.PrivateData.NodeName
	if nodeName == "" {
		nodeName = common.NodeName
	}
	consumption := model.BillingConsumption{
		Log: model.Log{UserId: task.UserId, Username: username, CreatedAt: common.GetTimestamp(),
			TokenId: task.PrivateData.TokenId, ChannelId: task.GetBillingChannelId(), ModelName: CovertMjpActionToModelName(task.Action),
			Quota: actualQuota, Content: task.FailReason, Other: string(encoded)},
		CountUsage: task.Status == "SUCCESS", LogEnabled: task.Status == "FAILURE" || common.LogConsumeEnabled,
		ExportEnabled: common.LogConsumeEnabled && common.DataExportEnabled, NodeName: nodeName,
	}
	if snapshot := task.PrivateData.BillingContext; snapshot != nil && task.Status == "SUCCESS" {
		consumption.AttemptID = snapshot.AttemptID
		consumption.Usage, consumption.Charge = snapshot.Usage, snapshot.Charge
	}
	won, err := model.PersistMidjourneyCompletion(ctx, task, fromStatus, consumption)
	if err != nil || !won {
		return won, err
	}
	_, err = recoverOneBillingRecovery(ctx, task.PrivateData.SubmissionRequestID)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Midjourney completion accounting remains pending (row_id=%d): %v", task.Id, err))
		return true, err
	}
	task.Quota = actualQuota
	return true, nil
}
