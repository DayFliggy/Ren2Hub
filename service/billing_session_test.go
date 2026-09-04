package service

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingBillingFunding struct {
	mu           sync.Mutex
	settleDeltas []int
	refunds      int
	refunded     chan struct{}
	refundOnce   sync.Once
}

type retryableBillingFunding struct {
	mu          sync.Mutex
	settleCalls int
	refundCalls int
	settleFails int
	refundFails int
}

func (f *retryableBillingFunding) Source() string       { return BillingSourceWallet }
func (f *retryableBillingFunding) PreConsume(int) error { return nil }
func (f *retryableBillingFunding) Settle(int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settleCalls++
	if f.settleFails > 0 {
		f.settleFails--
		return assert.AnError
	}
	return nil
}
func (f *retryableBillingFunding) Refund() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refundCalls++
	if f.refundFails > 0 {
		f.refundFails--
		return assert.AnError
	}
	return nil
}

func (f *recordingBillingFunding) Source() string { return BillingSourceWallet }

func (f *recordingBillingFunding) PreConsume(int) error { return nil }

func (f *recordingBillingFunding) Settle(delta int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settleDeltas = append(f.settleDeltas, delta)
	return nil
}

func (f *recordingBillingFunding) Refund() error {
	f.mu.Lock()
	f.refunds++
	f.mu.Unlock()
	f.refundOnce.Do(func() { close(f.refunded) })
	return nil
}

func (f *recordingBillingFunding) snapshot() ([]int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.settleDeltas...), f.refunds
}

func TestBillingSessionSettleAndRefundAreIdempotent(t *testing.T) {
	funding := &recordingBillingFunding{refunded: make(chan struct{})}
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
		funding:          funding,
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}

	require.NoError(t, session.Settle(140))
	require.NoError(t, session.Settle(140))
	deltas, refunds := funding.snapshot()
	assert.Equal(t, []int{40}, deltas)
	assert.Zero(t, refunds)

	ctx, _ := gin.CreateTestContext(nil)
	session.Refund(ctx)
	_, refunds = funding.snapshot()
	assert.Zero(t, refunds)
}

func TestBillingSessionRefundIsIssuedOnceAfterFinalFailure(t *testing.T) {
	funding := &recordingBillingFunding{refunded: make(chan struct{})}
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
		funding:          funding,
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	ctx, _ := gin.CreateTestContext(nil)

	session.Refund(ctx)
	session.Refund(ctx)
	select {
	case <-funding.refunded:
	case <-time.After(time.Second):
		t.Fatal("billing refund was not issued")
	}
	require.Eventually(t, func() bool {
		_, refunds := funding.snapshot()
		return refunds == 1
	}, time.Second, 10*time.Millisecond)
	assert.False(t, session.NeedsRefund())
}

func TestBillingSessionSettlementFailureCanBeRetriedWithoutRepeatingFunding(t *testing.T) {
	funding := &retryableBillingFunding{settleFails: 1}
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
		funding:          funding,
		preConsumedQuota: 100,
	}
	assert.Error(t, session.Settle(140))
	assert.False(t, session.settled)
	assert.NoError(t, session.Settle(140))
	assert.True(t, session.settled)
	funding.mu.Lock()
	assert.Equal(t, 2, funding.settleCalls)
	funding.mu.Unlock()
}

func TestBillingSessionRefundRetriesOnlyPendingComponents(t *testing.T) {
	funding := &retryableBillingFunding{refundFails: 1}
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{IsPlayground: true},
		funding:          funding,
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	ctx, _ := gin.CreateTestContext(nil)
	session.Refund(ctx)
	assert.True(t, session.NeedsRefund())
	session.Refund(ctx)
	assert.False(t, session.NeedsRefund())
	funding.mu.Lock()
	assert.Equal(t, 2, funding.refundCalls)
	funding.mu.Unlock()
}

func TestBillingSessionReservePersistsRecoveryAmountsWithReservation(t *testing.T) {
	truncate(t)
	const userID = 801
	const tokenID = 801
	const requestID = "billing-reserve-atomic-test"
	seedUser(t, userID, 60)
	seedToken(t, tokenID, userID, "billing-reserve-atomic-key", 60)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", 40).Error)

	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID:     requestID,
		UserID:        userID,
		TokenID:       tokenID,
		Source:        BillingSourceWallet,
		TokenRequired: true,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 40))

	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{UserId: userID, TokenId: tokenID, TokenKey: "billing-reserve-atomic-key"},
		funding:          &WalletFunding{userId: userID, consumed: 40},
		recoveryID:       requestID,
		preConsumedQuota: 40,
		tokenConsumed:    40,
	}
	require.NoError(t, session.Reserve(70))
	require.NoError(t, session.Reserve(70))
	require.NoError(t, session.Reserve(90))

	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, int64(90), recovery.FundingAmount)
	assert.Equal(t, int64(90), recovery.TokenAmount)
	assert.Equal(t, int64(0), recovery.ExtraAmount)
	assert.Equal(t, 90, session.GetPreConsumedQuota())

	var marker model.BillingRecoveryAdjustment
	require.NoError(t, model.DB.Where("request_id = ? AND component = ? AND operation = ?", requestID, "reserve:70", model.BillingRecoveryOperationReserve).First(&marker).Error)
	assert.Equal(t, int64(30), marker.Amount)
	var nextMarker model.BillingRecoveryAdjustment
	require.NoError(t, model.DB.Where("request_id = ? AND component = ? AND operation = ?", requestID, "reserve:90", model.BillingRecoveryOperationReserve).First(&nextMarker).Error)
	assert.Equal(t, int64(20), nextMarker.Amount)

	var user model.User
	var token model.Token
	require.NoError(t, model.DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").Where("id = ?", tokenID).First(&token).Error)
	assert.Equal(t, 10, user.Quota)
	assert.Equal(t, 10, token.RemainQuota)
	assert.Equal(t, 90, token.UsedQuota)
}
