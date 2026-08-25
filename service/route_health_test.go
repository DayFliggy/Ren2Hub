package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestClassifyRouteErrorSeparatesKeyModelAndStreamFailures(t *testing.T) {
	assert.Equal(t, RouteErrorKey, ClassifyRouteError(401, "", "", false).Class)
	assert.True(t, ClassifyRouteError(401, "", "", false).MarkKey)
	assert.True(t, ClassifyRouteError(401, "", "", false).Failoverable)
	assert.Equal(t, RouteErrorKey, ClassifyRouteError(403, "invalid_api_key", "", false).Class)
	assert.Equal(t, RouteErrorModel, ClassifyRouteError(404, "model_not_found", "", false).Class)
	assert.Equal(t, RouteErrorModel, ClassifyRouteError(400, "unsupported_model", "", false).Class)
	assert.True(t, ClassifyRouteError(404, "model_not_found", "", false).MarkCapability)
	assert.True(t, ClassifyRouteError(404, "model_not_found", "", false).Failoverable)
	assert.Equal(t, RouteErrorStreamStarted, ClassifyRouteError(502, "", "", true).Class)
	assert.False(t, ClassifyRouteError(502, "", "", true).Failoverable)
	assert.False(t, CanRouteFailover(ClassifyRouteError(503, "", "", false), true, false))
	assert.False(t, CanRouteFailover(ClassifyRouteError(503, "", "", false), false, true))
	assert.True(t, CanRouteFailover(ClassifyRouteError(503, "", "", false), false, false))
}

func TestRouteHealthStateMachineUsesEpochAndCooldown(t *testing.T) {
	now := time.Unix(1000, 0)
	policy := RouteHealthPolicy{FailureThreshold: 2, Cooldown: 10 * time.Second}
	health := model.ChannelHealth{ChannelID: 1, Model: "gpt-5", KeyScope: "channel"}
	health = ObserveRouteHealthFailure(health, policy, now)
	assert.Equal(t, model.RouteHealthStateClosed, health.State)
	health = ObserveRouteHealthFailure(health, policy, now)
	assert.Equal(t, model.RouteHealthStateOpen, health.State)
	assert.Equal(t, int64(1010), health.CooldownUntil)
	assert.False(t, CanUseRouteHealth(health, now))
	health = EnterRouteHealthHalfOpen(health, time.Unix(1010, 0))
	assert.Equal(t, model.RouteHealthStateHalfOpen, health.State)
	health = ObserveRouteHealthFailure(health, policy, time.Unix(1011, 0))
	assert.Equal(t, model.RouteHealthStateOpen, health.State)
	assert.Equal(t, int64(1021), health.CooldownUntil)
	health = EnterRouteHealthHalfOpen(health, time.Unix(1021, 0))
	assert.Equal(t, model.RouteHealthStateHalfOpen, health.State)
	health = ObserveRouteHealthSuccess(health, time.Unix(1022, 0))
	assert.Equal(t, model.RouteHealthStateClosed, health.State)
	assert.Equal(t, 0, health.FailureCount)
}

func TestDefaultRouteRetryBudgetSeparatesSameResourceAndFailover(t *testing.T) {
	budget := DefaultRouteRetryBudget()
	transient := ClassifyRouteError(503, "", "", false)
	assert.False(t, budget.Allows(transient, RouteRetrySameKey, RouteRetryCounters{TotalAttempts: 1}))
	assert.True(t, budget.Allows(transient, RouteRetrySameChannel, RouteRetryCounters{TotalAttempts: 1}))
	assert.False(t, budget.Allows(transient, RouteRetrySameChannel, RouteRetryCounters{TotalAttempts: 2, SameChannelAttempts: 1}))
	assert.True(t, budget.Allows(transient, RouteRetryFailover, RouteRetryCounters{TotalAttempts: 2, FailoverAttempts: 1}))
	assert.False(t, budget.Allows(transient, RouteRetryFailover, RouteRetryCounters{TotalAttempts: 3}))
	assert.Equal(t, 125*time.Millisecond, RouteBackoff(0, 0, time.Second, 0.5))
	assert.Equal(t, time.Second, RouteBackoff(0, time.Second, 5*time.Second, 0.5))
	assert.LessOrEqual(t, RouteBackoff(4, 10*time.Second, 100*time.Millisecond, 1), 100*time.Millisecond)
}

func TestParseRetryAfterAndDeadlineClipping(t *testing.T) {
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
		ok    bool
	}{
		{name: "empty", value: "", ok: false},
		{name: "seconds", value: "3", want: 3 * time.Second, ok: true},
		{name: "http date", value: now.Add(2 * time.Second).Format(http.TimeFormat), want: 2 * time.Second, ok: true},
		{name: "expired date", value: now.Add(-time.Second).Format(http.TimeFormat), ok: true},
		{name: "negative", value: "-1", ok: false},
		{name: "invalid", value: "not-a-date", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseRetryAfter(tt.value, now)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
	assert.Equal(t, 100*time.Millisecond, RouteBackoff(0, 3*time.Second, 100*time.Millisecond, 0))
}

func TestRouteHealthMetricsAndKeyScopeAreSeparated(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: 1, Model: "gpt-5", KeyScope: "", State: model.RouteHealthStateClosed,
		FailureCount: 1, HealthEpoch: 1, LastLatencyMS: 240, FirstTokenLatencyMS: 80,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: 1, Model: "gpt-5", KeyScope: RouteKeyScope("secret-key"), State: model.RouteHealthStateOpen,
		FailureCount: 3, HealthEpoch: 2,
	}).Error)
	errorRate, latency, err := RouteHealthMetrics(context.Background(), 1, "gpt-5")
	require.NoError(t, err)
	assert.InDelta(t, 1.0/3.0, errorRate, 0.001)
	assert.Equal(t, float64(240), latency)
	_, _, ttft, err := RouteHealthMetricsWithTTFT(context.Background(), 1, "gpt-5")
	require.NoError(t, err)
	assert.Equal(t, float64(80), ttft)
	assert.NotEqual(t, RouteKeyScope("secret-key"), "secret-key")
}

func TestRouteHealthUsableAllowsOneHalfOpenProbe(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: 2, Model: "gpt-5", KeyScope: "", State: model.RouteHealthStateOpen,
		FailureCount: 3, CooldownUntil: 1000, HealthEpoch: 4,
	}).Error)

	usable, epoch, err := RouteHealthUsable(context.Background(), 2, "gpt-5", time.Unix(1000, 0))
	require.NoError(t, err)
	assert.True(t, usable)
	assert.Equal(t, int64(5), epoch)

	usable, epoch, err = RouteHealthUsable(context.Background(), 2, "gpt-5", time.Unix(1001, 0))
	require.NoError(t, err)
	assert.False(t, usable)
	assert.Equal(t, int64(5), epoch)

	var health model.ChannelHealth
	require.NoError(t, db.Where("channel_id = ? AND model = ? AND key_scope = ?", 2, "gpt-5", "").First(&health).Error)
	assert.Equal(t, model.RouteHealthStateHalfOpen, health.State)
}

func TestObserveLiveRouteErrorKeepsKeyAndCapabilityScopesSeparate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))

	require.NoError(t, ObserveLiveRouteError(context.Background(), 3, "gpt-5", 404, "model_not_found", "", false))
	var aggregate model.ChannelHealth
	require.NoError(t, db.Where("channel_id = ? AND model = ? AND key_scope = ?", 3, "gpt-5", "").First(&aggregate).Error)
	assert.Equal(t, model.RouteHealthStateOpen, aggregate.State)

	require.NoError(t, ObserveLiveRouteErrorForKey(context.Background(), 3, "gpt-5", "", 401, "", "", false))
	var keyHealthCount int64
	require.NoError(t, db.Model(&model.ChannelHealth{}).Where("channel_id = ? AND model = ? AND key_scope <> ?", 3, "gpt-5", "").Count(&keyHealthCount).Error)
	assert.Zero(t, keyHealthCount)

	require.NoError(t, ObserveLiveRouteErrorForKey(context.Background(), 3, "gpt-5", "secret-key", 401, "", "", false))
	var keyHealth model.ChannelHealth
	require.NoError(t, db.Where("channel_id = ? AND model = ? AND key_scope = ?", 3, "gpt-5", RouteKeyScope("secret-key")).First(&keyHealth).Error)
	assert.Equal(t, model.RouteHealthStateOpen, keyHealth.State)

	channel := &model.Channel{Id: 3, Key: "secret-key\nhealthy-key", ChannelInfo: model.ChannelInfo{IsMultiKey: true}}
	excluded, err := UnavailableRouteKeyIndexes(context.Background(), channel, "gpt-5", time.Now())
	require.NoError(t, err)
	assert.Contains(t, excluded, 0)
	assert.NotContains(t, excluded, 1)
	singleKeyChannel := &model.Channel{Id: 3, Key: "secret-key"}
	excluded, err = UnavailableRouteKeyIndexes(context.Background(), singleKeyChannel, "gpt-5", time.Now())
	require.NoError(t, err)
	assert.Contains(t, excluded, 0)

	require.NoError(t, ObserveLiveRouteSuccessForKey(context.Background(), 3, "gpt-5", "secret-key", 20, 5))
	excluded, err = UnavailableRouteKeyIndexes(context.Background(), channel, "gpt-5", time.Now())
	require.NoError(t, err)
	assert.NotContains(t, excluded, 0)

	require.NoError(t, ObserveLiveRouteSuccessForKey(context.Background(), 3, "gpt-5", "healthy-key", 20, 5))
	require.NoError(t, db.Model(&model.ChannelHealth{}).Where("channel_id = ? AND model = ? AND key_scope = ?", 3, "gpt-5", RouteKeyScope("healthy-key")).Count(&keyHealthCount).Error)
	assert.Zero(t, keyHealthCount)
}
