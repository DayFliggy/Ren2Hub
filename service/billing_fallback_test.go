package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A final observation that cannot be priced still ends the request: the
// cumulative Realtime snapshot when one exists, otherwise the frozen pre-send
// estimate. Repeated final calls never charge twice and never leak raw inputs.
func TestUnpricedFinalUsageSettlesAtBestPersistedEvidence(t *testing.T) {
	for _, test := range []struct {
		path   string
		quota  int
		reason string
	}{
		{path: "text", quota: 5, reason: billingFallbackPricingFailed},
		{path: "audio", quota: 5, reason: billingFallbackPricingFailed},
		{path: "realtime_reserve", quota: 5, reason: billingFallbackNoTerminal},
		{path: "realtime_final", quota: 5, reason: billingFallbackPricingFailed},
		{path: "realtime_collected", quota: 35},
	} {
		t.Run(test.path, func(t *testing.T) {
			c, info := reservedRealtimeBillingSession(t)
			info.PriceData.Quote = billingTestQuote(t, `c == 13 ? 1 / (c - 13) : p * 2 + c * 10`)
			billingTestEstimate(t, info)
			require.Nil(t, PrepareBillingForSelectedModel(c, info))
			info.BillingRequestInput = &billingexpr.RequestInput{Headers: map[string]string{"X-Private-Fixture": "private-fixture"}, Body: []byte(`{"prompt":"private-fixture"}`)}
			usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 13, TotalTokens: 23}
			realtime := &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 13, TotalTokens: 23, Observation: confirmedRealtimeObservation()}
			info.MarkValidOutput()
			if test.path == "realtime_collected" {
				require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}))
			}
			for i := 0; i < 2; i++ {
				switch test.path {
				case "text":
					PostTextConsumeQuota(c, info, usage, nil)
				case "audio":
					PostAudioConsumeQuota(c, info, usage, nil)
				case "realtime_reserve":
					require.Error(t, PreWssConsumeQuota(c, info, realtime))
					continue
				case "realtime_final", "realtime_collected":
					PostWssConsumeQuota(c, info, info.OriginModelName, realtime, nil)
				}
				require.NoError(t, info.BillingSettlementError)
			}
			info.Billing.Refund(c)
			status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
			require.NoError(t, err)
			assert.EqualValues(t, model.BillingRecoveryStatusSettled, status)

			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			require.NoError(t, model.DB.First(&token, info.TokenId).Error)
			assert.EqualValues(t, 100-test.quota, user.Quota)
			assert.EqualValues(t, 100-test.quota, token.RemainQuota)
			assert.EqualValues(t, test.quota, user.UsedQuota)
			assert.Zero(t, user.ReservedQuota)
			assert.EqualValues(t, 1, user.RequestCount)

			var record model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
			assert.Equal(t, model.BillingRecoveryStateCompleted, record.ConsumptionState)
			assert.NotContains(t, record.ConsumptionSnapshot, "private-fixture")
			assert.NotContains(t, record.LastError, "c - 13")
			var settled billing.Usage
			require.NoError(t, common.UnmarshalJsonStr(string(record.UsageSnapshot), &settled))
			if test.reason != "" {
				assert.Equal(t, billing.UsageSourceRequestEstimate, settled.Source)
			} else {
				assert.EqualValues(t, 5, settled.OutputTokens, "the last cumulative snapshot is charged")
			}

			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ?", info.RequestId).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.EqualValues(t, test.quota, logs[0].Quota)
			assert.NotContains(t, logs[0].Content, "c - 13")
			assert.NotContains(t, logs[0].Other, "private-fixture")
			if test.reason != "" {
				var other map[string]any
				require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
				adminInfo, ok := other["admin_info"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, test.reason, adminInfo["billing_fallback_reason"])
			}
			backlog, err := model.ReadBillingRecoveryBacklog(context.Background(), time.Now().Unix()+1)
			require.NoError(t, err)
			assert.Zero(t, backlog.Pending)
		})
	}
}

// Unconfirmed token counters settle as observed, without the pre-send
// reservation buffer. Usage that cannot express the price settles at the
// frozen estimate. Each final charge is counted under its evidence basis.
func TestUnconfirmedTextUsageSettlesAtBestAvailableEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		usage  *dto.Usage
		call   bool
		quota  int
		frozen bool
		basis  string
	}{
		{name: "estimated token counters", usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20, UsageSource: "estimated_partial"}, quota: 20, basis: billing.SettlementEstimated},
		{name: "fixed fee without terminal success", usage: &dto.Usage{UsageSource: "missing"}, call: true, quota: 20, frozen: true, basis: billing.SettlementFrozenEstimate},
		{name: "duration cannot price tokens", usage: &dto.Usage{PromptTokens: 90, UsageSource: "provider_duration"}, quota: 5, frozen: true, basis: billing.SettlementFrozenEstimate},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, info := reservedRealtimeBillingSession(t)
			if test.call {
				info.PriceData.UsePrice, info.IsStream = true, true
				info.PriceData.Quote = billingTestCallQuote(t)
				info.PriceData.QuotaToPreConsume = 20
				billingTestEstimate(t, info)
			}
			require.Nil(t, PrepareBillingForSelectedModel(c, info))
			info.MarkValidOutput()
			before := billing.ReadOperationalMetrics().Settlements
			PostTextConsumeQuota(c, info, test.usage, nil)
			require.NoError(t, info.BillingSettlementError)

			var user model.User
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			assert.EqualValues(t, 100-test.quota, user.Quota)
			assert.EqualValues(t, test.quota, user.UsedQuota)
			assert.Zero(t, user.ReservedQuota)
			var record model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&record).Error)
			assert.Equal(t, model.BillingRecoveryStateCompleted, record.ConsumptionState)
			var settled billing.Usage
			require.NoError(t, common.UnmarshalJsonStr(string(record.UsageSnapshot), &settled))
			assert.Equal(t, test.frozen, settled.Source == billing.UsageSourceRequestEstimate)
			assert.False(t, settled.Confirmed())
			after := billing.ReadOperationalMetrics().Settlements
			assert.Equal(t, before[test.basis]+1, after[test.basis])
		})
	}
}

// Recovery decides "never sent" from its own reads. The release recheck under
// the row lock refuses a request whose dispatch has persisted an estimate.
func TestUnsentReleaseRechecksDispatchUnderLock(t *testing.T) {
	_, session := durableSession(t, 40)
	session.stopRecoveryHeartbeat()
	_, err := releaseUnsentBilling(session.recoveryID)
	require.ErrorIs(t, err, model.ErrBillingRecoveryStateConflict)
	recovery, err := loadBillingRecovery(session.recoveryID)
	require.NoError(t, err)
	assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status)
	assert.EqualValues(t, 40, recovery.FundingAmount)

	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "unsent_user", AffCode: "unsent", Quota: 100, Status: common.UserStatusEnabled}).Error)
	_, err = model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: "unsent", UserID: 2, Source: BillingSourceWallet})
	require.NoError(t, err)
	require.NoError(t, model.Reserve("unsent", 40))
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), "unsent", "unsent:attempt", 0, billingTestQuote(t, "p * 2")))
	status, err := releaseUnsentBilling("unsent")
	require.NoError(t, err)
	assert.Equal(t, model.BillingRecoveryStatusRefunded, status)
	assert.Equal(t, 100, getUserQuota(t, 2))
}

// A row that cannot be recovered moves behind waiting rows instead of
// occupying every batch.
func TestFailedRecoveryDoesNotStarveOtherRows(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 100)
	future := common.GetTimestamp() + 3600
	require.NoError(t, model.DB.Create(&model.BillingRecovery{RequestID: "legacy-source", UserID: 1, Source: "legacy", Status: model.BillingRecoveryStatusActive,
		PreConsumeState: model.BillingRecoveryPreConsumeCommitted, ConsumptionState: model.BillingRecoveryStateUsagePending, UpdatedAt: 1}).Error)
	require.NoError(t, model.DB.Create(&model.BillingRecovery{RequestID: "never-sent", UserID: 1, Source: BillingSourceWallet, Status: model.BillingRecoveryStatusActive,
		PreConsumeState: model.BillingRecoveryPreConsumeCommitted, UpdatedAt: 2}).Error)
	first, err := RecoverStaleBillingRecoveries(context.Background(), future, 1)
	require.NoError(t, err)
	assert.Equal(t, BillingRecoverySummary{Scanned: 1, Failed: 1}, first)
	second, err := RecoverStaleBillingRecoveries(context.Background(), future, 1)
	require.NoError(t, err)
	assert.Equal(t, BillingRecoverySummary{Scanned: 1, Refunded: 1}, second)
	recovery, err := loadBillingRecovery("never-sent")
	require.NoError(t, err)
	assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
}

// Losing one side of an already persisted estimate is corruption, not proof
// that dispatch never happened. Recovery must retain the reserved funds.
func TestDamagedAttemptEstimateCannotAuthorizeRecoveryRelease(t *testing.T) {
	for _, column := range []string{"estimate_charge", "estimate_usage", "quote_hash"} {
		t.Run(column, func(t *testing.T) {
			_, session := durableSession(t, 40)
			session.stopRecoveryHeartbeat()
			require.NoError(t, model.DB.Model(&model.BillingAttempt{}).
				Where("id = ?", session.relayInfo.BillingAttemptID).Update(column, "").Error)

			status, err := recoverOneBillingRecovery(context.Background(), session.recoveryID)
			require.Error(t, err)
			assert.Equal(t, model.BillingRecoveryStatusActive, status)
			var user model.User
			require.NoError(t, model.DB.First(&user, session.relayInfo.UserId).Error)
			assert.Equal(t, []common.Quota{60, 40, 0}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
			var events int64
			require.NoError(t, model.DB.Model(&model.BillingEvent{}).
				Where("request_id = ? AND operation IN ?", session.recoveryID, []string{"settle", "release"}).Count(&events).Error)
			assert.Zero(t, events)
		})
	}
}

// Two attempts can share both their immutable quote and creation second while
// dynamic request inputs produce different estimates. Recovery owes the last
// dispatched attempt, never whichever primary key happens to sort first.
func TestRecoveryChargesLatestAttemptWithTheSameQuote(t *testing.T) {
	_, session := durableSession(t, 40)
	info := session.relayInfo
	session.stopRecoveryHeartbeat()
	latestID := info.RequestId + ":retry"
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), info.RequestId, latestID, 0, info.PriceData.Quote))
	estimate, charge, err := model.EvaluateBilling(info.PriceData.Quote,
		billing.Usage{InputTokens: 15, ContextTokens: 15, Estimated: true, Source: billing.UsageSourceRequestEstimate},
		billingexpr.RequestInput{}, info.RequestId, latestID, "estimate")
	require.NoError(t, err)
	require.NoError(t, model.PersistBillingEstimate(latestID, estimate, charge))
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Where("request_id = ?", info.RequestId).Update("created_at", 1700000000).Error)
	// A delayed exit path from the previous attempt cannot replace the current
	// one, even when both quotes are byte-for-byte identical.
	require.ErrorIs(t, model.PrepareFrozenEstimateConsumption(info.RequestId, info.BillingAttemptID, model.Log{}, true), model.ErrBillingEventConflict)

	for range 2 {
		status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
		require.NoError(t, err)
		assert.Equal(t, model.BillingRecoveryStatusSettled, status)
	}
	assert.Equal(t, 85, getUserQuota(t, 1))
	assert.Equal(t, 15, getTokenUsedQuota(t, 1))
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", info.RequestId).Error)
	var consumption model.BillingConsumption
	require.NoError(t, common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption))
	assert.Equal(t, latestID, consumption.AttemptID)
	var events int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", info.RequestId, "settle").Count(&events).Error)
	assert.EqualValues(t, 1, events)
}

func TestRecoveryWithoutAttemptMarkerRequiresUniqueEvidence(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		name := "unique"
		if multiple {
			name = "ambiguous"
		}
		t.Run(name, func(t *testing.T) {
			_, session := durableSession(t, 40)
			session.stopRecoveryHeartbeat()
			info := session.relayInfo
			if multiple {
				require.NoError(t, model.PrepareBillingAttempt(context.Background(), info.RequestId, "later-without-estimate", 0, info.PriceData.Quote))
			}
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("request_id = ?", info.RequestId).
				Update("consumption_snapshot", "").Error)
			status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
			if multiple {
				require.Error(t, err)
				assert.Equal(t, model.BillingRecoveryStatusActive, status)
				var user model.User
				require.NoError(t, model.DB.First(&user, info.UserId).Error)
				assert.Equal(t, []common.Quota{60, 40, 0}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
				return
			}
			require.NoError(t, err)
			assert.Equal(t, model.BillingRecoveryStatusSettled, status)
			assert.Equal(t, 60, getUserQuota(t, info.UserId))
			assert.Equal(t, 40, getTokenUsedQuota(t, info.TokenId))
		})
	}
}

func TestRecoveryCannotReleaseDispatchWithDamagedState(t *testing.T) {
	_, session := durableSession(t, 40)
	session.stopRecoveryHeartbeat()
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("request_id = ?", session.recoveryID).
		Update("consumption_state", "").Error)
	_, err := recoverOneBillingRecovery(context.Background(), session.recoveryID)
	require.Error(t, err)
	var user model.User
	require.NoError(t, model.DB.First(&user, session.relayInfo.UserId).Error)
	assert.Equal(t, []common.Quota{60, 40, 0}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
}
