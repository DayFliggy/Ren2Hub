package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRouteShadowDiagnosticsUsesSealedPrimaryDatabaseWindow(t *testing.T) {
	primary, logs, now := setupRouteShadowDiagnosticsTest(t)
	windowStart := now.UTC().Truncate(time.Hour).Add(-7 * 24 * time.Hour)
	seedSealedShadowWindow(t, primary, windowStart, now.UTC().Truncate(time.Hour), []model.RouteShadowHourlyObservation{
		{Scope: model.RouteShadowObservationModel, ModelName: "gpt-5", ShadowDecisions: 2, ShadowInitialDecisions: 2, CapabilityResolved: 2},
		{Scope: model.RouteShadowObservationModel, ModelName: "claude-opus-5", ShadowDecisions: 1, ShadowInitialDecisions: 1, CapabilityResolved: 1},
	})
	require.NoError(t, logs.Create([]model.Log{
		{CreatedAt: windowStart.Add(time.Hour).Unix(), Type: model.LogTypeConsume, ModelName: "gpt-5"},
		{CreatedAt: windowStart.Add(time.Hour).Unix(), Type: model.LogTypeConsume, ModelName: "gpt-5"},
		{CreatedAt: windowStart.Add(time.Hour).Unix(), Type: model.LogTypeConsume, ModelName: "claude-opus-5"},
	}).Error)

	diagnostics := GetRouteShadowDiagnostics(context.Background())
	assert.False(t, diagnostics.CoverageDataUnavailable)
	assert.False(t, diagnostics.ShadowDataUnavailable)
	assert.Equal(t, "primary_database_hourly_aggregate", diagnostics.ShadowDataSource)
	assert.True(t, diagnostics.Durable.WindowComplete)
	assert.Equal(t, int64(3), diagnostics.CoreModelRequestCount)
	assert.Equal(t, int64(3), diagnostics.CoreModelShadowDecisionCount)
	assert.Equal(t, 1.0, diagnostics.CoreModelCoverage)
	assert.Equal(t, 1.0, diagnostics.CoreModelShadowCoverage)
	assert.Equal(t, 1.0, diagnostics.CoreModelLabResolution)
	assert.ElementsMatch(t, []string{"gpt-5", "claude-opus-5"}, diagnostics.CoreModels)
}

func TestRouteShadowDiagnosticsRejectsGapAndDataLoss(t *testing.T) {
	primary, logs, now := setupRouteShadowDiagnosticsTest(t)
	windowStart := now.UTC().Truncate(time.Hour).Add(-7 * 24 * time.Hour)
	windowEnd := now.UTC().Truncate(time.Hour)
	seedSealedShadowWindow(t, primary, windowStart, windowEnd, []model.RouteShadowHourlyObservation{
		{Scope: model.RouteShadowObservationModel, ModelName: "gpt-5", ShadowInitialDecisions: 1, CapabilityResolved: 1},
	})
	require.NoError(t, logs.Create(&model.Log{CreatedAt: windowStart.Add(time.Hour).Unix(), Type: model.LogTypeConsume, ModelName: "gpt-5"}).Error)

	require.NoError(t, primary.Where("hour_start = ? AND scope = ?", windowStart.Add(2*time.Hour).Unix(), model.RouteShadowObservationGlobal).Delete(&model.RouteShadowHourlyObservation{}).Error)
	assert.True(t, GetRouteShadowDiagnostics(context.Background()).ShadowDataUnavailable)

	require.NoError(t, primary.Create(&model.RouteShadowHourlyObservation{
		HourStart: windowStart.Add(2 * time.Hour).Unix(), InstanceID: "boot-a", Scope: model.RouteShadowObservationGlobal, SealedAt: now.Unix(), DataLossPossible: true, UpdatedAt: now.Unix(),
	}).Error)
	assert.True(t, GetRouteShadowDiagnostics(context.Background()).ShadowDataUnavailable)
}

func TestRouteShadowDiagnosticsDoesNotUseProcessLocalResolutionAsEvidence(t *testing.T) {
	_, logs, now := setupRouteShadowDiagnosticsTest(t)
	windowStart := now.UTC().Truncate(time.Hour).Add(-7 * 24 * time.Hour)
	require.NoError(t, logs.Create(&model.Log{CreatedAt: windowStart.Add(time.Hour).Unix(), Type: model.LogTypeConsume, ModelName: "gpt-5"}).Error)
	observeShadowDecision(RouteShadowDecision{NormalizedRequestModel: "gpt-5", LabSlug: "openai"})

	diagnostics := GetRouteShadowDiagnostics(context.Background())
	assert.False(t, diagnostics.CoverageDataUnavailable)
	assert.True(t, diagnostics.ShadowDataUnavailable)
	assert.False(t, diagnostics.Durable.WindowComplete)
}

func TestRouteShadowDecisionObservationCountsResolvedBeforeEligibility(t *testing.T) {
	delta := routeShadowDecisionObservation(RouteShadowDecision{
		NormalizedRequestModel: "gpt-5",
		HasMixed:               true,
		RetryAttempt:           0,
		DifferenceReasons:      []string{ShadowReasonDifferentPriority},
		ShadowCandidates: []RouteShadowCandidate{{
			LabSlug:      "openai",
			FilterReason: ShadowFilterGroupForbidden,
		}},
		FilterReasonCounts: map[string]int{
			ShadowFilterGroupForbidden:    1,
			ShadowFilterUnknownCapability: 2,
		},
	})
	assert.Equal(t, int64(1), delta.ShadowDecisions)
	assert.Equal(t, int64(1), delta.ShadowInitialDecisions)
	assert.Equal(t, int64(1), delta.CapabilityResolved)
	assert.Equal(t, int64(1), delta.MixedDecisions)
	assert.Equal(t, int64(1), delta.UnauthorizedFiltered)
	assert.Equal(t, int64(2), delta.UnknownFiltered)
	assert.Zero(t, delta.UnknownAdmitted)
	assert.Zero(t, delta.UnauthorizedAdmitted)
}

func TestDurableRouteShadowMetricsUsesPersistentRefreshHistogram(t *testing.T) {
	metrics := durableRouteShadowMetrics(routeShadowAggregateSnapshot{Global: model.RouteShadowHourlyObservation{
		RefreshLagCount:  20,
		RefreshLagLE1S:   18,
		RefreshLagLE60S:  1,
		RefreshLagGT300S: 1,
	}})
	assert.True(t, metrics.RefreshLagP95Known)
	assert.Equal(t, int64(60_000), metrics.RefreshLagP95MS)

	metrics = durableRouteShadowMetrics(routeShadowAggregateSnapshot{})
	assert.False(t, metrics.RefreshLagP95Known)
}

func TestAddRouteShadowObservationHeartbeatCreatesOnlyOneGlobalRowPerHour(t *testing.T) {
	pending := make(map[routeShadowObservationKey]model.RouteShadowHourlyObservation)
	hour := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC).Unix()
	addRouteShadowObservationHeartbeat(pending, "boot-a", hour)
	addRouteShadowObservationHeartbeat(pending, "boot-a", hour)
	addRouteShadowObservationHeartbeat(pending, "boot-a", hour+int64(time.Hour/time.Second))

	require.Len(t, pending, 2)
	for _, row := range pending {
		assert.Equal(t, model.RouteShadowObservationGlobal, row.Scope)
		assert.Equal(t, "boot-a", row.InstanceID)
		assert.Empty(t, row.ModelName)
	}
}

func setupRouteShadowDiagnosticsTest(t *testing.T) (*gorm.DB, *gorm.DB, time.Time) {
	t.Helper()
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainDB, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalNow := routeShadowDiagnosticsNow
	primary, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s-primary?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	logs, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s-logs?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = primary, logs
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	now := time.Date(2026, time.August, 26, 12, 30, 0, 0, time.UTC)
	routeShadowDiagnosticsNow = func() time.Time { return now }
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainDB, originalLogType)
		routeShadowDiagnosticsNow = originalNow
		if sqlDB, closeErr := primary.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
		if sqlDB, closeErr := logs.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, primary.AutoMigrate(&model.RouteShadowHourlyObservation{}))
	require.NoError(t, logs.AutoMigrate(&model.Log{}))
	return primary, logs, now
}

func seedSealedShadowWindow(t *testing.T, db *gorm.DB, start, end time.Time, modelRows []model.RouteShadowHourlyObservation) {
	t.Helper()
	sealedAt := end.Add(time.Minute).Unix()
	rows := make([]model.RouteShadowHourlyObservation, 0, int(end.Sub(start)/time.Hour)+len(modelRows))
	for hour := start; hour.Before(end); hour = hour.Add(time.Hour) {
		rows = append(rows, model.RouteShadowHourlyObservation{
			HourStart: hour.Unix(), InstanceID: "boot-a", Scope: model.RouteShadowObservationGlobal, SealedAt: sealedAt, UpdatedAt: sealedAt,
		})
	}
	for _, row := range modelRows {
		row.HourStart = start.Unix()
		row.InstanceID = "boot-a"
		row.SealedAt = sealedAt
		row.UpdatedAt = sealedAt
		rows = append(rows, row)
	}
	require.NoError(t, db.Create(&rows).Error)
}
