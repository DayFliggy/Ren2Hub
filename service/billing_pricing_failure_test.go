package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedExpressionRetainsReservationForReconciliation(t *testing.T) {
	for _, path := range []string{"text", "audio", "realtime_reserve", "realtime_final"} {
		t.Run(path, func(t *testing.T) {
			c, info := realtimeBillingSession(t)
			expr := `c == 13 ? float("invalid-price") : p * 2 + c * 10`
			require.NoError(t, billing_setting.SmokeTestExpr(expr))
			info.PriceData.Quote = billingTestQuote(t, expr)
			require.Nil(t, PrepareBillingForSelectedModel(c, info))
			info.BillingRequestInput = &billingexpr.RequestInput{Headers: map[string]string{"Authorization": "private-fixture"}, Body: []byte(`{"private":"private-fixture"}`)}
			usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 13, TotalTokens: 23}
			info.MarkValidOutput()
			realtime := &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 13, TotalTokens: 23}
			for i := 0; i < 2; i++ {
				switch path {
				case "text":
					PostTextConsumeQuota(c, info, usage, nil)
				case "audio":
					PostAudioConsumeQuota(c, info, usage, "")
				case "realtime_reserve":
					require.Error(t, PreWssConsumeQuota(c, info, realtime))
				case "realtime_final":
					PostWssConsumeQuota(c, info, info.OriginModelName, realtime, "")
				}
				require.ErrorContains(t, info.BillingSettlementError, "billing expression evaluation failed")
			}
			info.Billing.Refund(c)
			status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
			require.NoError(t, err)
			assert.EqualValues(t, model.BillingRecoveryStatusReconciliation, status)
			assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
			assert.ErrorIs(t, model.UpdateBillingRecoverySettlement(info.RequestId, 0), model.ErrBillingRecoveryStateConflict)
			var record model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
			assert.Contains(t, record.UsageSnapshot, `"output_tokens":13`)
			assert.Contains(t, record.QuoteSnapshot, "invalid-price")

			assert.NotContains(t, record.ConsumptionSnapshot, "private-fixture")
			assert.NotContains(t, record.LastError, "invalid-price")
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			require.NoError(t, model.DB.First(&token, info.TokenId).Error)
			assert.EqualValues(t, 95, user.Quota, "only the existing 5 quota reservation remains")
			assert.EqualValues(t, 95, token.RemainQuota)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.RequestCount)
			var logCount int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", info.RequestId).Count(&logCount).Error)
			assert.Zero(t, logCount, "no guessed final consumption log")
			backlog, err := model.ReadBillingRecoveryBacklog(context.Background(), time.Now().Unix()+1)
			require.NoError(t, err)
			assert.EqualValues(t, int64(1), backlog.Pending)
		})
	}
}
