package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayChannelPolicyRefreshUsesRemainingWaitBudget(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	require.NoError(t, db.Create(&model.Channel{Id: 82, Status: common.ChannelStatusEnabled, CapacityTotal: 1}).Error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holder, _, err := AcquireGatewayChannelPermit(ctx, "holder", 82, false, nil)
	require.NoError(t, err)
	defer holder.Release()
	budget := NewGatewayWaitBudget(time.Second)
	budget.consume(900 * time.Millisecond)
	var observedDeadline bool
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("slow_queued_policy", func(tx *gorm.DB) {
		if tx.Statement.Table != "options" {
			return
		}
		_, observedDeadline = tx.Statement.Context.Deadline()
		if assert.True(t, observedDeadline, "the policy read shares the remaining queue deadline") {
			<-tx.Statement.Context.Done()
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove("slow_queued_policy")) })
	permit, _, err := AcquireGatewayChannelPermit(ctx, "waiting", 82, true, budget)
	permit.Release()
	require.ErrorIs(t, err, ErrGatewayWaitExpired)
	assert.True(t, observedDeadline)
	assert.Zero(t, budget.Remaining())
	assert.NoError(t, ctx.Err(), "queue expiration must not replace the logical request context")
	snapshot := ReadGatewayAdmissionRuntime(ChannelAdmissionKey(82))
	assert.EqualValues(t, 1, snapshot.InFlight)
	assert.Zero(t, snapshot.Queued)
}

func TestGatewayRoutingPolicyRefreshesRatesWithoutExtendingWait(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	require.NoError(t, db.Create(&model.Channel{Id: 81, Status: common.ChannelStatusEnabled, CapacityTotal: 1}).Error)
	config := model.DefaultGatewayConfig()
	config.ChannelRates[81] = model.GatewayProviderRateLimit{RPM: 1}
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	policy, err := ResolveGatewayAdmissionPolicy(config, model.User{})
	require.NoError(t, err)
	budget := NewGatewayWaitBudget(time.Second)
	budget.consume(200 * time.Millisecond)
	ctx := WithGatewayAdmissionContext(context.Background(), policy, budget)
	reservation, err := ReserveGatewayProviderRate(ctx, common.RDB, []GatewayProviderRateScope{{ID: GatewayChannelRateScopeID(81), RPM: 1}}, 0)
	require.NoError(t, err)
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, reservation, 0, true, true, 0))
	candidates := []RouteSelectionCandidate{{ChannelID: 81}}
	filterKnownProviderUnavailable(ctx, candidates)
	assert.Equal(t, "provider_rate_limited", candidates[0].FilterReason)
	config.ChannelRates[81] = model.GatewayProviderRateLimit{RPM: 2}
	config.ChannelQueueSize, config.WaitMS = 7, 4000
	require.NoError(t, model.SaveGatewayConfig(config))
	candidates[0].FilterReason = ""
	filterKnownProviderUnavailable(ctx, candidates)
	assert.Empty(t, candidates[0].FilterReason, "a queued request must not skip newly available quota using its entry snapshot")
	current, enabled, err := GatewayRoutingPolicy(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, 7, current.ChannelQueueSize)
	assert.Equal(t, policy.Wait, current.Wait)
	assert.Equal(t, 800*time.Millisecond, budget.Remaining())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = GatewayRoutingPolicy(canceled)
	require.ErrorIs(t, err, context.Canceled)
}
