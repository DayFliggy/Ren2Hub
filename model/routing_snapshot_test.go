package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPublishChannelCapabilitySnapshotFencesAndRetainsRecentVersions(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&ChannelModelCapability{}, &ChannelCapabilitySnapshot{}))

	for version := 1; version <= 4; version++ {
		expected := ChannelCapabilitySnapshotFence{}
		if version > 1 {
			expected = ChannelCapabilitySnapshotFence{ActiveVersion: int64(version - 1), SourceHash: fmt.Sprintf("hash-%d", version-1), CatalogVersion: "catalog-1"}
		}
		err := PublishChannelCapabilitySnapshot(context.Background(), 7, expected, fmt.Sprintf("hash-%d", version), "catalog-1", []ChannelModelCapability{{
			RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", State: RouteCapabilityStateEligible,
		}})
		require.NoError(t, err)
	}

	var snapshot ChannelCapabilitySnapshot
	require.NoError(t, db.Where("channel_id = ?", 7).First(&snapshot).Error)
	assert.Equal(t, int64(4), snapshot.ActiveVersion)
	var count int64
	require.NoError(t, db.Model(&ChannelModelCapability{}).Where("channel_id = ?", 7).Count(&count).Error)
	assert.Equal(t, int64(3), count)

	active, err := FindActiveChannelCapabilities(context.Background(), []int{7}, "gpt-5", "openai")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, int64(4), active[0].SnapshotVersion)

	replay, err := FindChannelCapabilitySnapshotVersion(context.Background(), 7, 2)
	require.NoError(t, err)
	require.Len(t, replay, 1)
	assert.Equal(t, int64(2), replay[0].SnapshotVersion)
	_, err = FindChannelCapabilitySnapshotVersion(context.Background(), 7, 1)
	assert.ErrorIs(t, err, ErrCapabilitySnapshotNotFound)

	expected := ChannelCapabilitySnapshotFence{ActiveVersion: 4, SourceHash: "hash-4", CatalogVersion: "catalog-1"}
	assert.ErrorIs(t, MarkChannelCapabilityRefreshFailure(7, ChannelCapabilitySnapshotFence{ActiveVersion: 4, SourceHash: "wrong", CatalogVersion: "catalog-1"}, "failed-hash", "catalog-1"), ErrCapabilitySnapshotConflict)
	assert.ErrorIs(t, MarkChannelCapabilityRefreshFailure(7, ChannelCapabilitySnapshotFence{ActiveVersion: 4, SourceHash: "hash-4", CatalogVersion: "wrong"}, "failed-hash", "catalog-1"), ErrCapabilitySnapshotConflict)
	require.NoError(t, MarkChannelCapabilityRefreshFailure(7, expected, "hash-4", "catalog-1"))
	require.NoError(t, db.Where("channel_id = ?", 7).First(&snapshot).Error)
	assert.Equal(t, int64(4), snapshot.ActiveVersion)
	assert.Equal(t, RouteCapabilityRefreshActive, snapshot.RefreshStatus)
	assert.Equal(t, "hash-4", snapshot.LastFailedSourceHash)
	assert.Equal(t, "catalog-1", snapshot.LastFailedCatalogVersion)
	active, err = FindActiveChannelCapabilities(context.Background(), []int{7}, "gpt-5", "openai")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, int64(4), active[0].SnapshotVersion)

	require.NoError(t, PublishChannelCapabilitySnapshot(context.Background(), 7, expected, "hash-5", "catalog-1", []ChannelModelCapability{{
		RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", State: RouteCapabilityStateEligible,
	}}))
	err = PublishChannelCapabilitySnapshot(context.Background(), 7, expected, "hash-stale", "catalog-1", []ChannelModelCapability{{
		RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", State: RouteCapabilityStateEligible,
	}})
	assert.ErrorIs(t, err, ErrCapabilitySnapshotConflict)
	assert.ErrorIs(t, MarkChannelCapabilityRefreshFailure(7, expected, "stale-hash", "stale-catalog"), ErrCapabilitySnapshotConflict)
	require.NoError(t, db.Where("channel_id = ?", 7).First(&snapshot).Error)
	assert.Equal(t, int64(5), snapshot.ActiveVersion)
	assert.Equal(t, RouteCapabilityRefreshActive, snapshot.RefreshStatus)
	assert.Equal(t, "hash-4", snapshot.LastFailedSourceHash)
	assert.Equal(t, "catalog-1", snapshot.LastFailedCatalogVersion)
}

func TestPublishChannelCapabilitySnapshotConcurrentCASHasSingleWinner(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s/routing.db?_journal_mode=WAL&_busy_timeout=10000", t.TempDir())), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	require.NoError(t, db.AutoMigrate(&ChannelModelCapability{}, &ChannelCapabilitySnapshot{}))
	initialCapability := []ChannelModelCapability{{RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", State: RouteCapabilityStateEligible}}
	require.NoError(t, PublishChannelCapabilitySnapshot(context.Background(), 71, ChannelCapabilitySnapshotFence{}, "hash-1", "catalog-1", initialCapability))
	expected := ChannelCapabilitySnapshotFence{ActiveVersion: 1, SourceHash: "hash-1", CatalogVersion: "catalog-1"}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			capability := []ChannelModelCapability{{RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", State: RouteCapabilityStateEligible}}
			var result error
			for attempt := 0; attempt < 20; attempt++ {
				result = PublishChannelCapabilitySnapshot(context.Background(), 71, expected,
					fmt.Sprintf("hash-%d", index+2), "catalog-1", capability)
				if result == nil || errors.Is(result, ErrCapabilitySnapshotConflict) ||
					(!strings.Contains(strings.ToLower(result.Error()), "locked") && !strings.Contains(strings.ToLower(result.Error()), "deadlock")) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			results <- result
		}(index)
	}
	close(start)
	wg.Wait()
	close(results)

	successes, conflicts := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrCapabilitySnapshotConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent publish error: %v", result)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)

	var snapshot ChannelCapabilitySnapshot
	require.NoError(t, db.Where("channel_id = ?", 71).First(&snapshot).Error)
	assert.Equal(t, int64(2), snapshot.ActiveVersion)
	var count int64
	require.NoError(t, db.Model(&ChannelModelCapability{}).Where("channel_id = ?", 71).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestMarkChannelCapabilityRefreshFailureCreatesInitialFailureSnapshot(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&ChannelCapabilitySnapshot{}))

	require.NoError(t, MarkChannelCapabilityRefreshFailure(88, ChannelCapabilitySnapshotFence{}, "failed-source", "catalog-1"))
	var snapshot ChannelCapabilitySnapshot
	require.NoError(t, db.Where("channel_id = ?", 88).First(&snapshot).Error)
	assert.Zero(t, snapshot.ActiveVersion)
	assert.Equal(t, RouteCapabilityRefreshFailed, snapshot.RefreshStatus)
	assert.Equal(t, "failed-source", snapshot.LastFailedSourceHash)
	assert.Equal(t, "catalog-1", snapshot.LastFailedCatalogVersion)
}

type legacyChannelModelCapability struct {
	ID             int    `gorm:"primaryKey"`
	ChannelID      int    `gorm:"uniqueIndex:channel_model_capability_version"`
	RequestModel   string `gorm:"uniqueIndex:channel_model_capability_version"`
	ActualModel    string `gorm:"uniqueIndex:channel_model_capability_version"`
	CatalogVersion string `gorm:"uniqueIndex:channel_model_capability_version"`
}

func (legacyChannelModelCapability) TableName() string { return "channel_model_capabilities" }

func TestPrepareChannelCapabilityMigrationDropsLegacyDerivedRows(t *testing.T) {
	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, db.AutoMigrate(&legacyChannelModelCapability{}))
	require.NoError(t, db.Create(&legacyChannelModelCapability{
		ChannelID: 7, RequestModel: "gpt-5", ActualModel: "gpt-5", CatalogVersion: "catalog-1",
	}).Error)
	require.True(t, db.Migrator().HasIndex(&ChannelModelCapability{}, "channel_model_capability_version"))
	require.NoError(t, prepareChannelCapabilityMigration())

	var remaining int64
	require.NoError(t, db.Model(&legacyChannelModelCapability{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
	assert.False(t, db.Migrator().HasIndex(&ChannelModelCapability{}, "channel_model_capability_version"))
	require.NoError(t, db.AutoMigrate(&ChannelModelCapability{}, &ChannelCapabilitySnapshot{}))
}
