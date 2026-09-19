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
	"gorm.io/gorm"
)

var billingPricingGuardScenarios = []string{
	"pricing_guard_write_failure", "pricing_failure_write_failure", "pricing_success",
	"pricing_explicit_refund", "pricing_refund_resume", "pricing_free", "pricing_realtime",
	"pricing_realtime_failure", "pricing_queue_fairness", "pricing_consumption_write_failure",
}

func TestBillingPricingGuardLifecycle(t *testing.T) {
	for _, scenario := range billingPricingGuardScenarios {
		t.Run(scenario, func(t *testing.T) {
			c, info := realtimeBillingSession(t)
			runBillingPricingGuardScenario(t, c, info, scenario)
		})
	}
}

// The same accounting contracts run against isolated SQLite, MySQL and Postgres.
func runBillingPricingGuardScenario(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, scenario string) {
	t.Helper()
	expr := `c == 13 ? float("invalid-price") : p * 2 + c * 2`
	info.PriceData.Quote = billingTestQuote(t, expr)
	info.PriceData.QuotaToPreConsume = 5
	if scenario == "pricing_free" {
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		info.Billing = nil
		info.FinalPreConsumedQuota = 0
		info.RequestId += "-free"
		info.PriceData.FreeModel = true
		info.PriceData.QuotaToPreConsume = 0
		info.PriceData.Quote = billingTestQuote(t, "0")
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", info.UserId).Update("available_quota", 0).Error)
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", info.TokenId).Update("available_quota", 0).Error)
	}
	var stale model.BillingRecovery
	if scenario == "pricing_explicit_refund" {
		require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&stale).Error)
	}
	if scenario == "pricing_guard_write_failure" {
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
	require.NotNil(t, info.Billing)
	var pending model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
	require.EqualValues(t, model.BillingRecoveryStateUsagePending, pending.ConsumptionState)
	assert.NotEmpty(t, pending.QuoteSnapshot)
	assert.Empty(t, pending.ConsumptionSnapshot)

	switch scenario {
	case "pricing_failure_write_failure", "pricing_realtime_failure":
		reserved := 5
		if scenario == "pricing_realtime_failure" {
			require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 1, TotalTokens: 11}))
			reserved = 11
		}
		remove, writes := failBillingStateWrite(t, "status", model.BillingRecoveryStatusReconciliation)
		PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 13, TotalTokens: 23}, nil)
		require.Error(t, info.BillingSettlementError)
		assert.Positive(t, *writes, "failed persistence must retain the prior send guard")
		remove()
		info.Billing.Refund(c)
		status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
		require.NoError(t, err)
		assert.EqualValues(t, model.BillingRecoveryStatusActive, status)
		assert.ErrorIs(t, model.MarkBillingRecoveryAutomaticRefundPending(info.RequestId), model.ErrBillingRecoveryStateConflict)
		assertBillingIntegrationBalances(t, info, 100-reserved, 100-reserved, 0, 0, 0, 0)
		require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
		if scenario == "pricing_realtime_failure" {
			assert.EqualValues(t, model.BillingRecoveryStateUsageCollecting, pending.ConsumptionState)
			var confirmed model.BillingConsumption
			require.NoError(t, common.UnmarshalJsonStr(pending.ConsumptionSnapshot, &confirmed))
			assert.EqualValues(t, reserved, confirmed.Log.Quota)
		} else {
			assert.EqualValues(t, model.BillingRecoveryStateUsagePending, pending.ConsumptionState)
			assert.NotEmpty(t, pending.QuoteSnapshot)
		}
		backlog, err := model.ReadBillingRecoveryBacklog(context.Background(), time.Now().Unix()+1)
		require.NoError(t, err)
		assert.Positive(t, backlog.Pending)
		// Recovery uses only database evidence and stays safe without the session.
		session := info.Billing
		info.Billing = nil
		defer func() { info.Billing = session }()
		status, err = recoverOneBillingRecovery(context.Background(), info.RequestId)
		require.NoError(t, err)
		assert.EqualValues(t, model.BillingRecoveryStatusActive, status)
	case "pricing_explicit_refund":
		_, err := recoverBillingRefund(stale)
		assert.ErrorIs(t, err, model.ErrBillingRecoveryStateConflict, "a worker that read before dispatch must recheck the guard under lock")
		info.Billing.Refund(c)
		require.NoError(t, info.BillingRefundError)
		assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
	case "pricing_refund_resume":
		remove, _ := failBillingStateWrite(t, "status", model.BillingRecoveryStatusRefunded)
		info.Billing.Refund(c)
		require.Error(t, info.BillingRefundError)
		remove()
		require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("request_id = ?", info.RequestId).Update("updated_at", 1).Error)
		rows, err := model.ListStaleBillingRecoveries(2, 1)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.EqualValues(t, info.RequestId, rows[0].RequestID)
		status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
		require.NoError(t, err)
		assert.EqualValues(t, model.BillingRecoveryStatusRefunded, status)
		assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
	case "pricing_queue_fairness":
		require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("request_id = ?", info.RequestId).Update("updated_at", 1).Error)
		require.NoError(t, model.DB.Create(&model.BillingRecovery{RequestID: info.RequestId + "-review", UserID: info.UserId,
			Source: BillingSourceWallet, Status: model.BillingRecoveryStatusReconciliation, UpdatedAt: 1}).Error)
		ordinary := info.RequestId + "-ordinary"
		require.NoError(t, model.DB.Create(&model.BillingRecovery{RequestID: ordinary, UserID: info.UserId,
			Source: BillingSourceWallet, Status: model.BillingRecoveryStatusActive, PreConsumeState: model.BillingRecoveryPreConsumeCommitted, UpdatedAt: 2}).Error)
		summary, err := RecoverStaleBillingRecoveries(context.Background(), 3, 1)
		require.NoError(t, err)
		assert.EqualValues(t, 1, summary.Scanned)
		assert.EqualValues(t, 1, summary.Refunded)
		backlog, err := model.ReadBillingRecoveryBacklog(context.Background(), 3)
		require.NoError(t, err)
		assert.EqualValues(t, int64(2), backlog.Pending, "manual work remains visible but cannot occupy the worker batch")
	case "pricing_success", "pricing_free", "pricing_realtime", "pricing_consumption_write_failure":
		quota := 20
		if scenario == "pricing_free" {
			quota = 0
		}
		var remove func()
		if scenario == "pricing_consumption_write_failure" {
			remove, _ = failBillingStateWrite(t, "consumption_state", model.BillingRecoveryStatePending)
		}
		if scenario == "pricing_realtime" {
			require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 1, TotalTokens: 11}))
			require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}))
			require.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
			PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 10, TotalTokens: 20}, "")
		} else {
			PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20}, nil)
		}
		if remove != nil {
			require.Error(t, info.BillingSettlementError)
			remove()
			assertBillingIntegrationBalances(t, info, 95, 95, 0, 0, 0, 0)
			assert.ErrorContains(t, model.ApplyBillingRecoveryConsumption(info.RequestId), "unconfirmed")
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
			assert.EqualValues(t, model.BillingRecoveryStatusActive, pending.Status)
			assert.EqualValues(t, model.BillingRecoveryStateUsagePending, pending.ConsumptionState)
			return
		}
		require.NoError(t, info.BillingSettlementError)
		info.Billing.Refund(c)
		wallet := 100 - quota
		if scenario == "pricing_free" {
			wallet = 0
		}
		assertBillingIntegrationBalances(t, info, wallet, wallet, quota, quota, 1, 1)
		require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
		assert.EqualValues(t, model.BillingRecoveryStateCompleted, pending.ConsumptionState)
	}
}

func failBillingStateWrite(t *testing.T, field, value string) (func(), *int) {
	t.Helper()
	const callback = "test:pricing-state-write-failure"
	writes := 0
	db := model.DB
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		values, _ := tx.Statement.Dest.(map[string]any)
		if tx.Statement.Table == "billing_recoveries" && values[field] == value {
			writes++
			tx.AddError(assert.AnError)
		}
	}))
	removed := false
	remove := func() {
		if !removed {
			require.NoError(t, db.Callback().Update().Remove(callback))
			removed = true
		}
	}
	t.Cleanup(remove)
	return remove, &writes
}
