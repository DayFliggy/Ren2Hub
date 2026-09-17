package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var billingDispatchGuardScenarios = []string{
	"dispatch_final_write_failure", "dispatch_per_call_write_failure", "dispatch_zero_precharge_write_failure",
	"dispatch_guard_write_failure", "dispatch_explicit_refund", "dispatch_refund_resume", "dispatch_realtime",
	"dispatch_subscription_write_failure", "dispatch_paid_zero_precharge_write_failure",
}

func TestBillingDispatchGuardLifecycle(t *testing.T) {
	for _, scenario := range billingDispatchGuardScenarios {
		t.Run(scenario, func(t *testing.T) {
			c, info := realtimeBillingSession(t)
			runBillingDispatchGuardScenario(t, c, info, scenario)
		})
	}
}

func runBillingDispatchGuardScenario(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, scenario string) {
	t.Helper()
	initialBacklog, err := model.ReadBillingRecoveryBacklog(context.Background(), time.Now().Unix()+1)
	require.NoError(t, err)
	info.PriceData.QuotaToPreConsume = 5
	quota := 20
	var subscription *model.UserSubscription
	if scenario == "dispatch_subscription_write_failure" || scenario == "dispatch_paid_zero_precharge_write_failure" {
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		info.Billing, info.FinalPreConsumedQuota = nil, 0
		info.RequestId += "-guarded"
		if scenario == "dispatch_subscription_write_failure" {
			require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
			plan := &model.SubscriptionPlan{Title: "dispatch-test", TotalAmount: 100, QuotaResetPeriod: "never"}
			require.NoError(t, model.DB.Create(plan).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id)
			subscription = &model.UserSubscription{UserId: info.UserId, PlanId: plan.Id, AmountTotal: 100,
				Status: "active", StartTime: time.Now().Unix(), EndTime: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, model.DB.Create(subscription).Error)
			t.Cleanup(func() {
				require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).Delete(&model.SubscriptionPreConsumeRecord{}).Error)
				require.NoError(t, model.DB.Delete(subscription).Error)
				require.NoError(t, model.DB.Delete(plan).Error)
				model.InvalidateSubscriptionPlanCache(plan.Id)
			})
			info.UserSetting.BillingPreference = "subscription_only"
		} else {
			info.PriceData.QuotaToPreConsume = 0
		}
	}
	if scenario == "dispatch_per_call_write_failure" {
		info.PriceData.UsePrice = true
		info.PriceData.ModelPrice = float64(quota) / common.QuotaPerUnit
		info.PriceData.QuotaToPreConsume = quota
	}
	if scenario == "dispatch_zero_precharge_write_failure" {
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		info.Billing, info.FinalPreConsumedQuota = nil, 0
		info.RequestId += "-free"
		info.PriceData.FreeModel = true
		info.PriceData.QuotaToPreConsume = 0
		quota = 0
	}
	if scenario == "dispatch_guard_write_failure" {
		remove, writes := failBillingStateWrite(t, "consumption_state", model.BillingRecoveryStateUsagePending)
		require.NotNil(t, PrepareBillingForSelectedModel(c, info))
		assert.Equal(t, 1, *writes)
		remove()
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
		return
	}
	var stale model.BillingRecovery
	if info.Billing != nil {
		require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&stale).Error)
	}
	require.Nil(t, PrepareBillingForSelectedModel(c, info))
	reserved := info.Billing.GetPreConsumedQuota()
	if subscription != nil {
		assert.Equal(t, BillingSourceSubscription, info.BillingSource)
	}
	var pending model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
	require.Equal(t, model.BillingRecoveryStateUsagePending, pending.ConsumptionState)
	var snapshot model.BillingDispatchSnapshot
	require.NoError(t, common.UnmarshalJsonStr(pending.ConsumptionSnapshot, &snapshot))
	assert.Equal(t, info.PriceData.ChannelRatio, snapshot.Price.ChannelRatio)
	assert.Equal(t, info.BillingModelName(), snapshot.Model)
	assert.Equal(t, common.QuotaPerUnit, snapshot.QuotaPerUnit)

	if scenario == "dispatch_explicit_refund" || scenario == "dispatch_refund_resume" {
		_, err := recoverBillingRefund(stale)
		assert.ErrorIs(t, err, model.ErrBillingRecoveryStateConflict)
		remove := func() {}
		if scenario == "dispatch_refund_resume" {
			remove, _ = failBillingStateWrite(t, "status", model.BillingRecoveryStatusRefunded)
		}
		info.Billing.Refund(c)
		if scenario == "dispatch_refund_resume" {
			require.Error(t, info.BillingRefundError)
		}
		remove()
		status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
		require.NoError(t, err)
		assert.Equal(t, model.BillingRecoveryStatusRefunded, status)
		assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
		return
	}
	info.MarkValidOutput()
	if scenario == "dispatch_realtime" {
		require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 1, TotalTokens: 11}))
		reserved = info.Billing.GetPreConsumedQuota()
		require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
		assert.Equal(t, model.BillingRecoveryStateUsageCollecting, pending.ConsumptionState)
		assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
	}
	remove, writes := failBillingStateWrite(t, "status", model.BillingRecoveryStatusSettlementPending)
	params := model.RecordConsumeLogParams{TokenId: info.TokenId, ChannelId: info.ChannelId, ModelName: info.BillingModelName(), PromptTokens: 10, CompletionTokens: 10, Quota: quota}
	settleBillingConsumption(c, info, params, true)
	require.Error(t, info.BillingSettlementError)
	assert.Equal(t, 6, *writes)
	remove()
	info.Billing.(*BillingSession).stopRecoveryHeartbeat()
	info.Billing.Refund(c)
	status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, model.BillingRecoveryStatusActive, status)
	assert.ErrorIs(t, model.MarkBillingRecoveryAutomaticRefundPending(info.RequestId), model.ErrBillingRecoveryStateConflict)
	wallet := 100 - reserved
	if subscription != nil {
		wallet = 100
		require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
		assert.Equal(t, int64(reserved), subscription.AmountUsed)
	}
	assertBillingIntegrationBalances(t, info, wallet, 100-reserved, 0, reserved, 0, 0)
	rows, err := model.ListStaleBillingRecoveries(time.Now().Unix()+1, 100)
	require.NoError(t, err)
	assert.Empty(t, rows, "unconfirmed usage must not occupy automatic recovery batches")
	backlog, err := model.ReadBillingRecoveryBacklog(context.Background(), time.Now().Unix()+1)
	require.NoError(t, err)
	assert.Equal(t, initialBacklog.Pending, backlog.Pending, "the unresolved request stays visible alongside earlier reconciliation records")

	// Retrying known final usage resolves the hold exactly once; the worker
	// itself must not infer this amount from the old reservation or snapshot.
	settleBillingConsumption(c, info, params, true)
	require.NoError(t, info.BillingSettlementError)
	status, err = recoverOneBillingRecovery(context.Background(), info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, model.BillingRecoveryStatusSettled, status)
	wallet = 100 - quota
	if subscription != nil {
		wallet = 100
		require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
		assert.Equal(t, int64(quota), subscription.AmountUsed)
	}
	assertBillingIntegrationBalances(t, info, wallet, 100-quota, quota, quota, 1, 1)
}
