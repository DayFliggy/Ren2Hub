package service

import (
	"context"
	"testing"

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
	"pricing_guard_write_failure", "pricing_success", "pricing_refund_resume", "pricing_free",
	"pricing_realtime", "pricing_consumption_write_failure",
}

func TestBillingPricingGuardLifecycle(t *testing.T) {
	for _, scenario := range billingPricingGuardScenarios {
		t.Run(scenario, func(t *testing.T) {
			c, info := reservedRealtimeBillingSession(t)
			runBillingPricingGuardScenario(t, c, info, scenario)
		})
	}
}

// The same accounting contracts run against isolated SQLite, MySQL and Postgres.
func runBillingPricingGuardScenario(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, scenario string) {
	t.Helper()
	expr := `c == 13 ? 1 / (c - 13) : p * 2 + c * 2`
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
	billingTestEstimate(t, info)
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
	var dispatch model.BillingConsumption
	require.NoError(t, common.UnmarshalJsonStr(pending.ConsumptionSnapshot, &dispatch))
	assert.Equal(t, info.BillingAttemptID, dispatch.AttemptID)
	assert.Empty(t, pending.ChargeSnapshot)
	assert.Empty(t, pending.ConsumptionFingerprint, "dispatch must not create a final charge")

	switch scenario {
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
	case "pricing_success", "pricing_free", "pricing_realtime", "pricing_consumption_write_failure":
		info.MarkValidOutput()
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
			PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 10, TotalTokens: 20, Observation: &dto.UsageObservation{Source: "provider", Complete: true, Terminal: "completed"}}, "")
		} else {
			PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20, UsageSource: "provider"}, nil)
		}
		if remove != nil {
			require.Error(t, info.BillingSettlementError)
			remove()
			// Statistics and logs never run ahead of a persisted settlement intent.
			assertBillingIntegrationBalances(t, info, 95, 95, 0, 0, 0, 0)
			require.Error(t, model.ApplyBillingRecoveryConsumption(info.RequestId))
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&pending).Error)
			assert.EqualValues(t, model.BillingRecoveryStatusActive, pending.Status)
			assert.EqualValues(t, model.BillingRecoveryStateUsagePending, pending.ConsumptionState)
			// Recovery then charges the frozen pre-send estimate exactly once.
			for range 2 {
				status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
				require.NoError(t, err)
				assert.EqualValues(t, model.BillingRecoveryStatusSettled, status)
			}
			assertBillingIntegrationBalances(t, info, 95, 95, 5, 5, 1, 1)
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
