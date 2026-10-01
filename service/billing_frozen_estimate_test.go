package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func frozenEstimateQuote(t *testing.T, multiplier string) *billing.Quote {
	t.Helper()
	quote := billingTestQuote(t, "p * 4")
	quote.PreConsumeMultiplier = multiplier
	quote.Hash = ""
	var err error
	quote.Hash, err = billing.Hash(quote)
	require.NoError(t, err)
	return quote
}

func TestNewFrozenEstimateRequestFallbackSettlesWithoutReservationBuffer(t *testing.T) {
	for _, test := range []struct {
		name        string
		multiplier  string
		reservation int
	}{
		{name: "double", multiplier: "2", reservation: 40},
		{name: "half", multiplier: "0.5", reservation: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 1, 100)
			seedToken(t, 1, 1, "frozen-request", 100)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			quote := frozenEstimateQuote(t, test.multiplier)
			info := &relaycommon.RelayInfo{RequestId: "frozen-request", BillingAttemptID: "frozen-request:attempt", UserId: 1, TokenId: 1, TokenKey: "frozen-request"}
			info.PriceData.Quote = quote
			info.BillingRequestInput = &billingexpr.RequestInput{}
			session, apiErr := NewBillingSession(c, info, test.reservation)
			require.Nil(t, apiErr)
			info.Billing = session
			t.Cleanup(session.stopRecoveryHeartbeat)
			require.NoError(t, model.PrepareBillingAttempt(context.Background(), info.RequestId, info.BillingAttemptID, 0, quote))
			usage, charge, err := model.EvaluateBilling(quote, billing.Usage{InputTokens: 10, ContextTokens: 10, Estimated: true, Source: billing.UsageSourceFrozenEstimate}, *info.BillingRequestInput, info.RequestId, info.BillingAttemptID, "estimate")
			require.NoError(t, err)
			require.EqualValues(t, 20, charge.FinalQuota)
			require.NoError(t, model.PersistBillingEstimate(info.BillingAttemptID, usage, charge))
			info.PriceData.Usage, info.PriceData.Charge = &usage, charge
			info.MarkValidOutput()
			info.UpstreamSent = true
			session.Refund(c)
			require.NoError(t, info.BillingSettlementError)
			assert.Equal(t, 80, getUserQuota(t, 1))
			assert.Equal(t, 20, getTokenUsedQuota(t, 1))
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", info.RequestId).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			assert.Equal(t, common.Quota(20-test.reservation), recovery.SettlementDelta)
		})
	}
}

func TestNewFrozenEstimateTaskFallbackSettlesWithoutReservationBuffer(t *testing.T) {
	for _, test := range []struct {
		name        string
		multiplier  string
		reservation int
	}{
		{name: "double", multiplier: "2", reservation: 40},
		{name: "half", multiplier: "0.5", reservation: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 1, 100)
			seedChannel(t, 1)
			task := makeTask(1, 1, test.reservation, 0, BillingSourceWallet)
			task.PrivateData.BillingContext.Quote = frozenEstimateQuote(t, test.multiplier)
			task.PrivateData.BillingContext.Usage = &billing.Usage{InputTokens: 10, ContextTokens: 10, Estimated: true, Source: billing.UsageSourceFrozenEstimate}
			acceptTestTask(t, task)
			task.Status = model.TaskStatusSuccess
			quota, reason, _, err := taskCompletionQuota(nil, task, nil)
			require.NoError(t, err)
			assert.EqualValues(t, 20, quota)
			assert.Equal(t, billing.UsageSourceFrozenEstimate, task.PrivateData.BillingContext.Usage.Source)
			won, err := completeTaskBilling(context.Background(), task, model.TaskStatusInProgress, quota, reason)
			require.NoError(t, err)
			assert.True(t, won)
			assert.Equal(t, 80, getUserQuota(t, 1))
			assert.EqualValues(t, 20, getChannelUsedQuota(t, 1))
			assert.Equal(t, 20, getTaskQuota(t, task.ID))
		})
	}
}
