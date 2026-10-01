package service

import (
	"bytes"
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingFallbackRedactsExpressionInputsFromApplicationLogs(t *testing.T) {
	for _, expression := range []string{
		`c > 0 ? int(header("X-Audit-Fixture")) : p * 2`,
		`c > 0 ? int(param("private_value")) : p * 2`,
	} {
		t.Run(expression, func(t *testing.T) {
			c, info := reservedRealtimeBillingSession(t)
			const privateValue = "private-billing-input-fixture"
			info.PriceData.Quote = billingTestQuote(t, expression)
			info.BillingRequestInput = &billingexpr.RequestInput{
				Headers: map[string]string{"X-Audit-Fixture": privateValue},
				Body:    []byte(`{"private_value":"private-billing-input-fixture"}`),
			}
			billingTestEstimate(t, info)
			require.Nil(t, PrepareBillingForSelectedModel(c, info))
			info.MarkValidOutput()
			_, _, evaluationErr := model.EvaluateBilling(info.PriceData.Quote,
				billing.Usage{InputTokens: 10, OutputTokens: 1, Source: "provider"},
				*info.BillingRequestInput, info.RequestId, info.BillingAttemptID, "settlement")
			require.EqualError(t, evaluationErr, "billing expression evaluation failed", "normal pricing already redacts raw VM errors")
			_, _, rawErr := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: 10, C: 1}, *info.BillingRequestInput)
			require.ErrorContains(t, rawErr, privateValue, "the fallback boundary is exercised with an actual sensitive VM error")

			var logs bytes.Buffer
			before := billing.ReadOperationalMetrics().Settlements[billing.SettlementFrozenEstimate]
			common.LogWriterMu.Lock()
			previous := gin.DefaultErrorWriter
			gin.DefaultErrorWriter = &logs
			common.LogWriterMu.Unlock()
			t.Cleanup(func() {
				common.LogWriterMu.Lock()
				gin.DefaultErrorWriter = previous
				common.LogWriterMu.Unlock()
			})
			// Future callers must not rely on the current calculator sanitizing
			// every cause before it reaches this exported service boundary.
			SettleBillingFallback(c, info, billingFallbackPricingFailed, rawErr)
			require.NoError(t, info.BillingSettlementError)
			common.LogWriterMu.Lock()
			logged := logs.String()
			common.LogWriterMu.Unlock()
			assert.Contains(t, logged, "reason=pricing_failed")
			assert.NotContains(t, logged, privateValue)
			assert.NotContains(t, logged, expression)
			var record model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, record.Status)
			assert.NotContains(t, record.ConsumptionSnapshot, privateValue)
			_, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
			require.NoError(t, err)
			assert.Equal(t, before+1, billing.ReadOperationalMetrics().Settlements[billing.SettlementFrozenEstimate])
		})
	}
}

func TestBillingMetricsCountOneCommittedChargeAcrossRepeatedDelivery(t *testing.T) {
	c, info := reservedRealtimeBillingSession(t)
	require.Nil(t, PrepareBillingForSelectedModel(c, info))
	info.MarkValidOutput()
	before := billing.ReadOperationalMetrics().Settlements[billing.SettlementEstimated]
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20, UsageSource: "estimated_partial"}
	for range 2 {
		PostTextConsumeQuota(c, info, usage, nil)
		require.NoError(t, info.BillingSettlementError)
	}
	_, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, before+1, billing.ReadOperationalMetrics().Settlements[billing.SettlementEstimated])
	var events int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", info.RequestId, "settle").Count(&events).Error)
	assert.EqualValues(t, 1, events)
}
