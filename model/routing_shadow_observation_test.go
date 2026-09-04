package model

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRouteShadowHourlyObservationMigrationAndAtomicAccumulate(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s/routing.db?_journal_mode=WAL&_busy_timeout=10000", t.TempDir())), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, migrateRoutingModels(db))
	require.True(t, db.Migrator().HasIndex(&RouteShadowHourlyObservation{}, "route_shadow_hourly_observation"))

	hour := time.Now().UTC().Truncate(time.Hour).Unix()
	deltas := []RouteShadowHourlyObservation{
		{HourStart: hour, InstanceID: "boot-a", Scope: RouteShadowObservationGlobal, ShadowDecisions: 1, EventAttempted: 1},
		{HourStart: hour, InstanceID: "boot-a", Scope: RouteShadowObservationModel, ModelName: "gpt-5", ShadowDecisions: 1, ShadowInitialDecisions: 1, CapabilityResolved: 1},
	}
	const workers = 8
	const increments = 25
	var wg sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range increments {
				if err := UpsertRouteShadowHourlyObservations(context.Background(), deltas); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	rows, err := ListRouteShadowHourlyObservations(context.Background(), hour, hour+int64(time.Hour/time.Second))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byScope := make(map[string]RouteShadowHourlyObservation, len(rows))
	for _, row := range rows {
		byScope[row.Scope] = row
	}
	expected := int64(workers * increments)
	assert.Equal(t, expected, byScope[RouteShadowObservationGlobal].ShadowDecisions)
	assert.Equal(t, expected, byScope[RouteShadowObservationGlobal].EventAttempted)
	assert.Equal(t, expected, byScope[RouteShadowObservationModel].ShadowInitialDecisions)
	assert.Equal(t, expected, byScope[RouteShadowObservationModel].CapabilityResolved)

	for _, delta := range deltas {
		delta.InstanceID = "boot-b"
		delta.ShadowDecisions = 1
		delta.ShadowInitialDecisions = 1
		delta.EventAttempted = 1
		delta.CapabilityResolved = 1
		require.NoError(t, UpsertRouteShadowHourlyObservations(context.Background(), []RouteShadowHourlyObservation{delta}))
	}
	rows, err = ListRouteShadowHourlyObservations(context.Background(), hour, hour+int64(time.Hour/time.Second))
	require.NoError(t, err)
	require.Len(t, rows, 4)
	var globalTotal int64
	for _, row := range rows {
		if row.Scope == RouteShadowObservationGlobal {
			globalTotal += row.ShadowDecisions
		}
	}
	assert.Equal(t, expected+1, globalTotal)
}

func TestRouteShadowHourlyObservationRejectsLateDeltaAsDataLoss(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, migrateRoutingModels(db))

	hour := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour).Unix()
	delta := RouteShadowHourlyObservation{HourStart: hour, InstanceID: "boot-a", Scope: RouteShadowObservationGlobal, ShadowDecisions: 1}
	require.NoError(t, UpsertRouteShadowHourlyObservations(context.Background(), []RouteShadowHourlyObservation{delta}))
	require.NoError(t, SealRouteShadowHourlyObservations(context.Background(), "boot-a", hour+int64(time.Hour/time.Second)))
	require.NoError(t, UpsertRouteShadowHourlyObservations(context.Background(), []RouteShadowHourlyObservation{delta}))

	rows, err := ListRouteShadowHourlyObservations(context.Background(), hour, hour+int64(time.Hour/time.Second))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), rows[0].ShadowDecisions)
	assert.True(t, rows[0].DataLossPossible)
}

func TestSealExpiredRouteShadowHourlyObservationsSealsPreviousInstance(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, migrateRoutingModels(db))

	hour := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour).Unix()
	require.NoError(t, db.Create(&RouteShadowHourlyObservation{
		HourStart: hour, InstanceID: "previous-boot", Scope: RouteShadowObservationGlobal, UpdatedAt: time.Now().Unix(),
	}).Error)
	require.NoError(t, SealExpiredRouteShadowHourlyObservations(context.Background(), hour+int64(time.Hour/time.Second)))

	var observation RouteShadowHourlyObservation
	require.NoError(t, db.Where("hour_start = ? AND instance_id = ?", hour, "previous-boot").First(&observation).Error)
	assert.Positive(t, observation.SealedAt)
}
