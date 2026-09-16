package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayBatchRevocationFencesEveryUserBeforeRefresh(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	users := []model.User{{Id: 9101, Username: "first", AffCode: "first", Status: common.UserStatusEnabled}, {Id: 9102, Username: "second", AffCode: "second", Status: common.UserStatusEnabled}}
	require.NoError(t, db.Create(&users).Error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := AdmissionPolicy{Capacity: 1, QueueSize: 1, Revision: admissionRevision.Load()}
	first, err := gatewayAdmission.TryAcquire(ctx, UserAdmissionKey(9101), policy)
	require.NoError(t, err)
	defer first.Release()
	second, err := gatewayAdmission.TryAcquire(ctx, UserAdmissionKey(9102), policy)
	require.NoError(t, err)
	defer second.Release()
	done := make(chan error, 1)
	go func() {
		permit, err := gatewayAdmission.Acquire(ctx, UserAdmissionKey(9102), policy, true)
		permit.Release()
		done <- err
	}()
	require.Eventually(t, func() bool { return gatewayAdmission.Snapshot(UserAdmissionKey(9102)).Queued == 1 }, time.Second, time.Millisecond)
	require.NoError(t, db.Model(&model.User{}).Where("id IN ?", []int{9101, 9102}).Update("status", common.UserStatusDisabled).Error)
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("block_admission_refresh", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			once.Do(func() { close(entered); <-resume })
		}
	}))
	finished := make(chan struct{})
	go func() { model.NotifyGatewayAdmissionChanged("user", 9101, 9102); close(finished) }()
	<-entered
	second.Release()
	assert.EqualValues(t, 1, gatewayAdmission.Snapshot(UserAdmissionKey(9102)).Queued, "later resources must be fenced before the first refresh performs I/O")
	close(resume)
	<-finished
	require.NoError(t, db.Callback().Query().Remove("block_admission_refresh"))
	require.ErrorIs(t, <-done, ErrAdmissionRuntime)
}

func TestGatewayProviderPolicyReadHonorsCancellation(t *testing.T) {
	setupChannelAdmissionTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadGatewayProviderRateScopes(ctx, 1, "fixture")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGatewayAdmissionCommittedUpdatesWakeExistingWaiters(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	config := model.DefaultGatewayConfig()
	config.UserConcurrency, config.UserQueueSize = 1, 2
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	user := model.User{Id: 901, Username: "admission-policy", Group: "team", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	common.RedisEnabled = false
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	policy, err := LoadGatewayAdmissionPolicy(ctx, user.Id)
	require.NoError(t, err)
	first, err := AcquireGatewayUserPermit(ctx, "first", user.Id, policy, NewGatewayWaitBudget(0))
	require.NoError(t, err)
	defer first.Release()
	type result struct {
		permit *Permit
		err    error
	}
	queue := make(chan result, 1)
	go func() {
		p, err := AcquireGatewayUserPermit(ctx, "second", user.Id, policy, NewGatewayWaitBudget(time.Second))
		queue <- result{p, err}
	}()
	require.Eventually(t, func() bool { return ReadGatewayAdmissionRuntime(UserAdmissionKey(user.Id)).Queued == 1 }, time.Second, time.Millisecond)
	config.UserConcurrency = 2
	require.NoError(t, model.SaveGatewayConfig(config))
	next := <-queue
	require.NoError(t, next.err)
	require.NotNil(t, next.permit)
	defer next.permit.Release()
	assert.EqualValues(t, 2, ReadGatewayAdmissionRuntime(UserAdmissionKey(user.Id)).InFlight)
	assert.EqualValues(t, 2, ReadGatewayAdmissionRuntime(UserAdmissionKey(user.Id)).ConfigVersion)
	policy, err = LoadGatewayAdmissionPolicy(ctx, user.Id)
	require.NoError(t, err)
	go func() {
		p, err := AcquireGatewayUserPermit(ctx, "third", user.Id, policy, NewGatewayWaitBudget(time.Second))
		queue <- result{p, err}
	}()
	require.Eventually(t, func() bool { return ReadGatewayAdmissionRuntime(UserAdmissionKey(user.Id)).Queued == 1 }, time.Second, time.Millisecond)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("status", common.UserStatusDisabled).Error)
	model.NotifyGatewayAdmissionChanged("user", user.Id)
	denied := <-queue
	denied.permit.Release()
	require.ErrorIs(t, denied.err, ErrAdmissionRuntime)
	assert.Zero(t, ReadGatewayAdmissionRuntime(UserAdmissionKey(user.Id)).Queued)
	assert.EqualValues(t, 2, ReadGatewayAdmissionRuntime(UserAdmissionKey(user.Id)).InFlight)
}

func TestGatewayChannelPolicyRefreshWakesWithoutNewTraffic(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	channel := model.Channel{Id: 902, Status: common.ChannelStatusEnabled, CapacityTotal: 1}
	require.NoError(t, db.Create(&channel).Error)
	common.RedisEnabled = false
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first, _, err := AcquireGatewayChannelPermit(ctx, "first", channel.Id, false, nil)
	require.NoError(t, err)
	defer first.Release()
	done := make(chan *Permit, 1)
	failures := make(chan error, 1)
	go func() {
		p, _, err := AcquireGatewayChannelPermit(ctx, "second", channel.Id, true, NewGatewayWaitBudget(time.Second))
		failures <- err
		done <- p
	}()
	require.Eventually(t, func() bool { return ReadGatewayAdmissionRuntime(ChannelAdmissionKey(channel.Id)).Queued == 1 }, time.Second, time.Millisecond)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("capacity_total", 2).Error)
	model.NotifyGatewayAdmissionChanged("channel", channel.Id)
	require.NoError(t, <-failures)
	second := <-done
	require.NotNil(t, second)
	defer second.Release()
	assert.EqualValues(t, 2, ReadGatewayAdmissionRuntime(ChannelAdmissionKey(channel.Id)).InFlight)
}
