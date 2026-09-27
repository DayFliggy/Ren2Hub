package service

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http/httptest"
	"testing"
)

func durableSession(t *testing.T, quota int) (*gin.Context, *BillingSession) {
	t.Helper()
	truncate(t)
	seedUser(t, 1, 100)
	seedToken(t, 1, 1, "session", 100)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RequestId: "session", UserId: 1, TokenId: 1, TokenKey: "session"}
	info.PriceData.Quote = billingTestQuote(t, "p * 2")
	session, err := NewBillingSession(c, info, quota)
	require.Nil(t, err)
	info.Billing = session
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), info.RequestId, info.RequestId+":attempt", 0, info.PriceData.Quote))
	t.Cleanup(session.stopRecoveryHeartbeat)
	return c, session
}
func TestBillingSessionSettleAndRefundAreIdempotent(t *testing.T) {
	c, s := durableSession(t, 40)
	prepareSessionSettlement(t, s, 140)
	require.NoError(t, s.Settle(140))
	require.NoError(t, s.Settle(140))
	require.Error(t, s.Settle(141))
	s.Refund(c)
	var u model.User
	var token model.Token
	require.NoError(t, model.DB.First(&u, 1).Error)
	require.NoError(t, model.DB.First(&token, 1).Error)
	assert.EqualValues(t, -40, u.Quota)
	assert.EqualValues(t, 140, u.UsedQuota)
	assert.Zero(t, u.ReservedQuota)
	assert.EqualValues(t, 140, token.UsedQuota)
	assert.Zero(t, token.ReservedQuota)
	var events int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("operation = ?", "settle").Count(&events).Error)
	assert.EqualValues(t, 1, events)
}
func TestBillingSessionFailureRollsBackBothAccountsAndRecovers(t *testing.T) {
	c, s := durableSession(t, 40)
	prepareSessionSettlement(t, s, 50)
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("token-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(assert.AnError)
		}
	}))
	require.Error(t, s.Settle(50))
	s.Refund(c)
	assert.Equal(t, 60, getUserQuota(t, 1))
	assert.Equal(t, 0, getTokenUsedQuota(t, 1))
	require.NoError(t, model.DB.Callback().Update().Remove("token-fail"))
	status, err := recoverOneBillingRecovery(context.Background(), s.recoveryID)
	require.NoError(t, err)
	assert.Equal(t, model.BillingRecoveryStatusSettled, status)
	assert.Equal(t, 50, getUserQuota(t, 1))
	assert.Equal(t, 50, getTokenUsedQuota(t, 1))
}
func TestBillingSessionZeroChargeDoesNotReserveMinimum(t *testing.T) {
	_, s := durableSession(t, 0)
	require.NoError(t, s.Reserve(0))
	assert.Zero(t, s.GetPreConsumedQuota())
	assert.Equal(t, 100, getUserQuota(t, 1))
}

func TestBillingSessionRequiresPublishedQuoteBeforeReservation(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 100)
	seedToken(t, 1, 1, "missing-quote", 100)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RequestId: "missing-quote", UserId: 1, TokenId: 1, TokenKey: "missing-quote"}
	session, err := NewBillingSession(c, info, 40)
	require.Nil(t, session)
	require.NotNil(t, err)
	assert.Equal(t, 503, err.StatusCode)
	assert.Equal(t, 100, getUserQuota(t, 1))
	var events, recoveries int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Count(&events).Error)
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
	assert.Zero(t, events)
	assert.Zero(t, recoveries)
}

func TestBillingSessionRetryRequiresPublishedQuoteBeforeIncreasingReservation(t *testing.T) {
	_, retry := durableSession(t, 40)
	retry.relayInfo.PriceData.Quote = nil
	require.Error(t, retry.Reserve(80))
	assert.Equal(t, 60, getUserQuota(t, 1))
	assert.Equal(t, 40, retry.GetPreConsumedQuota())
}

func prepareSessionSettlement(t *testing.T, session *BillingSession, quota int) {
	t.Helper()
	info := session.relayInfo
	billingTestFinalUsage(t, info, int64(quota), 0)
	require.NoError(t, model.PrepareBillingRecoveryConsumption(info.RequestId, model.BillingConsumption{
		Log:   model.Log{UserId: info.UserId, TokenId: info.TokenId, ModelName: info.PriceData.Quote.PriceVersion.Model, Quota: common.Quota(quota)},
		Usage: info.PriceData.Usage, Charge: info.PriceData.Charge,
	}))
}
func TestBillingSessionMoreExpensiveRetryMustReserve(t *testing.T) {
	_, s := durableSession(t, 40)
	require.NoError(t, s.Reserve(80))
	require.ErrorIs(t, s.Reserve(101), model.ErrBillingRecoveryFundingQuota)
	assert.Equal(t, 20, getUserQuota(t, 1))
	assert.Equal(t, 0, getTokenUsedQuota(t, 1))
}
func TestBillingSessionUnknownAcceptanceKeepsReservation(t *testing.T) {
	c, s := durableSession(t, 40)
	s.relayInfo.UpstreamSent = true
	s.Refund(c)
	assert.Equal(t, 60, getUserQuota(t, 1))
	var r model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", s.recoveryID).First(&r).Error)
	assert.Equal(t, model.BillingRecoveryStatusActive, r.Status)
}
func TestBillingSessionConfirmedUnsentRelease(t *testing.T) {
	c, s := durableSession(t, 40)
	s.Refund(c)
	s.Refund(c)
	assert.Equal(t, 100, getUserQuota(t, 1))
	assert.Zero(t, getTokenUsedQuota(t, 1))
	assert.False(t, s.NeedsRefund())
}

func TestBillingSessionAcceptedTaskCannotBeReleasedByContradictoryRejection(t *testing.T) {
	for _, staleFlag := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejection_flag_%t", staleFlag), func(t *testing.T) {
			c, s := durableSession(t, 40)
			s.relayInfo.UpstreamSent = true
			s.relayInfo.UpstreamAccepted = true
			s.relayInfo.UpstreamSideEffecting = true
			s.relayInfo.UpstreamRejected = staleFlag
			c.Set("gateway_last_attempt_outcome", AttemptOutcome{Acceptance: AttemptRejected})
			s.Refund(c)
			assert.Equal(t, 60, getUserQuota(t, 1))
			assert.True(t, s.NeedsRefund())
		})
	}
}

func TestBillingSessionExplicitTextRejectionReleasesReservation(t *testing.T) {
	c, s := durableSession(t, 40)
	s.relayInfo.UpstreamSent = true
	s.relayInfo.UpstreamAccepted = true // HTTP 200 can contain a provider safety rejection.
	c.Set("gateway_last_attempt_outcome", AttemptOutcome{Acceptance: AttemptRejected})
	s.Refund(c)
	assert.Equal(t, 100, getUserQuota(t, 1))
	assert.False(t, s.NeedsRefund())
}

var _ = common.MaxQuota
