package service

import (
	"sync"
	"testing"
	"time"

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
