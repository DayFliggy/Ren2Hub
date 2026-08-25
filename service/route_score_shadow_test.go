package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAttachRouteScoreShadowIsNoOpWhenDisabled(t *testing.T) {
	t.Setenv("ROUTE_SCORE_SHADOW_ENABLED", "false")
	decision := RouteShadowDecision{ShadowPreferredID: 1}
	AttachRouteScoreShadow(context.Background(), &decision)
	assert.False(t, decision.ScoreShadowEnabled)
	assert.Equal(t, 1, decision.ShadowPreferredID)
}

func TestAttachRouteScoreShadowRecordsDifferenceWithoutChangingStaticChoice(t *testing.T) {
	t.Setenv("ROUTE_SCORE_SHADOW_ENABLED", "true")
	previousDB := model.DB
	t.Cleanup(func() { model.DB = previousDB })

	db, err := gorm.Open(sqlite.Open("file:route-score-shadow?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: 1, Model: "gpt-5", KeyScope: "", State: model.RouteHealthStateClosed,
		FailureCount: 3, HealthEpoch: 1, LastLatencyMS: 2000, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: 2, Model: "gpt-5", KeyScope: "", State: model.RouteHealthStateClosed,
		FailureCount: 0, HealthEpoch: 1, LastLatencyMS: 100, UpdatedAt: now,
	}).Error)

	decision := RouteShadowDecision{
		NormalizedRequestModel: "gpt-5",
		ShadowPreferredID:      1,
		ShadowCandidates: []RouteShadowCandidate{
			{ChannelID: 1, Priority: 10, Weight: 1},
			{ChannelID: 2, Priority: 10, Weight: 1},
			{ChannelID: 3, Priority: 5, Weight: 100},
		},
	}
	AttachRouteScoreShadow(context.Background(), &decision)

	assert.True(t, decision.ScoreShadowEnabled)
	assert.Equal(t, 1, decision.ShadowPreferredID)
	assert.Equal(t, 2, decision.ScoreShadowPreferredID)
	assert.Equal(t, RouteScoreShadowDifferentSameLayer, decision.ScoreShadowDifference)
	assert.Empty(t, decision.ScoreShadowError)
	assert.NotNil(t, decision.ShadowCandidates[0].Score)
	assert.NotNil(t, decision.ShadowCandidates[1].Score)
	assert.NotNil(t, decision.ShadowCandidates[2].Score)

	require.NoError(t, db.Model(&model.ChannelHealth{}).Where("channel_id = ?", 1).Updates(map[string]any{
		"state": model.RouteHealthStateOpen, "cooldown_until": time.Now().Add(time.Minute).Unix(),
	}).Error)
	healthFiltered := RouteShadowDecision{
		NormalizedRequestModel: "gpt-5",
		ShadowPreferredID:      1,
		ShadowCandidates: []RouteShadowCandidate{
			{ChannelID: 1, Priority: 10, Weight: 1},
			{ChannelID: 2, Priority: 5, Weight: 1},
		},
	}
	AttachRouteScoreShadow(context.Background(), &healthFiltered)
	assert.Equal(t, 2, healthFiltered.ScoreShadowPreferredID)
	assert.Equal(t, RouteScoreShadowHealthDifference, healthFiltered.ScoreShadowDifference)
}
