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
	"dispatch_guard_write_failure", "dispatch_explicit_refund", "dispatch_realtime",
	"dispatch_paid_zero_precharge_write_failure",
}

func TestBillingDispatchGuardLifecycle(t *testing.T) {
	for _, scenario := range billingDispatchGuardScenarios {
		t.Run(scenario, func(t *testing.T) {
			c, info := reservedRealtimeBillingSession(t)
			runBillingDispatchGuardScenario(t, c, info, scenario)
		})
	}
}

func runBillingDispatchGuardScenario(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, scenario string) {
	t.Helper()
	info.PriceData.QuotaToPreConsume = 5
	quota := 20
	if scenario == "dispatch_paid_zero_precharge_write_failure" {
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		info.Billing, info.FinalPreConsumedQuota = nil, 0
		info.RequestId += "-guarded"
		info.PriceData.QuotaToPreConsume = 0
	}
	if scenario == "dispatch_per_call_write_failure" {
		info.PriceData.UsePrice = true
		info.PriceData.Quote = billingTestCallQuote(t)
		info.PriceData.QuotaToPreConsume = quota
	}
	if scenario == "dispatch_zero_precharge_write_failure" {
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		info.Billing, info.FinalPreConsumedQuota = nil, 0
		info.RequestId += "-free"
		info.PriceData.FreeModel = true
		info.PriceData.Quote = billingTestQuote(t, "0")
		info.PriceData.QuotaToPreConsume = 0
		quota = 0
	}
	billingTestEstimate(t, info)
	if scenario == "dispatch_guard_write_failure" {
		remove, writes := failBillingStateWrite(t, "consumption_state", model.BillingRecoveryStateUsagePending)
		require.NotNil(t, PrepareBillingForSelectedModel(c, info))
		assert.EqualValues(t, 1, *writes)
		remove()
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
		return
	}
	require.Nil(t, PrepareBillingForSelectedModel(c, info))
	reserved := info.Billing.GetPreConsumedQuota()
	var pending model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
	require.EqualValues(t, model.BillingRecoveryStateUsagePending, pending.ConsumptionState)
	assert.NotEmpty(t, pending.QuoteSnapshot)
	assert.Empty(t, pending.ConsumptionSnapshot)
	var attempts int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Where("request_id = ?", info.RequestId).Count(&attempts).Error)
	assert.EqualValues(t, 1, attempts)

	if scenario == "dispatch_explicit_refund" {
		// A dispatched request that produced no output releases its hold.
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
		require.NoError(t, err)
		assert.EqualValues(t, model.BillingRecoveryStatusRefunded, status)
		assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
		return
	}
	info.MarkValidOutput()
	if scenario == "dispatch_realtime" {
		require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 1, TotalTokens: 11}))
		reserved = info.Billing.GetPreConsumedQuota()
		require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
		assert.EqualValues(t, model.BillingRecoveryStateUsageCollecting, pending.ConsumptionState)
		assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
	}
	// This suite injects outbox persistence faults after final usage is known.
	// Replace the live estimate with complete, independently replayable evidence.
	billingTestFinalUsage(t, info, 10, 10)
	remove, writes := failBillingStateWrite(t, "status", model.BillingRecoveryStatusSettlementPending)
	params := model.RecordConsumeLogParams{TokenId: info.TokenId, ChannelId: info.ChannelId, ModelName: info.BillingModelName(), PromptTokens: 10, CompletionTokens: 10, Quota: common.Quota(quota)}
	settleBillingConsumption(c, info, params, true)
	require.Error(t, info.BillingSettlementError)
	assert.Positive(t, *writes)
	remove()
	// A failed settlement intent moves no money and cannot become a release.
	info.Billing.Refund(c)
	assertBillingIntegrationBalances(t, info, 100-reserved, 100-reserved, 0, 0, 0, 0)
	rows, err := model.ListStaleBillingRecoveries(time.Now().Unix()+1, 100)
	require.NoError(t, err)
	requestIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		requestIDs = append(requestIDs, row.RequestID)
	}
	assert.Contains(t, requestIDs, info.RequestId, "an unfinished request is finished by recovery, not left for an operator")

	if scenario == "dispatch_realtime" {
		// Recovery charges the last durable cumulative snapshot exactly once.
		for range 2 {
			status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
			require.NoError(t, err)
			assert.EqualValues(t, model.BillingRecoveryStatusSettled, status)
		}
		assertBillingIntegrationBalances(t, info, 100-reserved, 100-reserved, reserved, reserved, 1, 1)
		return
	}
	// Retrying known final usage before recovery settles it exactly once.
	settleBillingConsumption(c, info, params, true)
	require.NoError(t, info.BillingSettlementError)
	status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
	require.NoError(t, err)
	assert.EqualValues(t, model.BillingRecoveryStatusSettled, status)
	assertBillingIntegrationBalances(t, info, 100-quota, 100-quota, quota, quota, 1, 1)
}
