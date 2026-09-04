package model

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRoutingDatabaseIntegration(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		database  common.DatabaseType
		dialector func(string) gorm.Dialector
	}{
		{name: "mysql", env: "TEST_MYSQL_DSN", database: common.DatabaseTypeMySQL, dialector: func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
		{name: "postgres", env: "TEST_POSTGRES_DSN", database: common.DatabaseTypePostgreSQL, dialector: func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(test.env))
			if dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			originalDB := DB
			originalMainDB := common.MainDatabaseType()
			originalLogDB := common.LogDatabaseType()
			DB = db
			common.SetDatabaseTypes(test.database, test.database)
			t.Cleanup(func() {
				DB = originalDB
				common.SetDatabaseTypes(originalMainDB, originalLogDB)
			})

			require.NoError(t, migrateRoutingModels(db))
			require.NoError(t, migrateChannelCapabilityIndexes())
			require.True(t, db.Migrator().HasIndex(&ChannelModelCapability{}, "channel_model_capability_snapshot"))
			require.True(t, db.Migrator().HasIndex(&ChannelRoutePolicy{}, "channel_route_policy_model"))
			require.True(t, db.Migrator().HasIndex(&RouteShadowHourlyObservation{}, "route_shadow_hourly_observation"))

			channelID := 991001
			require.NoError(t, db.Where("channel_id = ?", channelID).Delete(&ChannelModelCapability{}).Error)
			require.NoError(t, db.Where("channel_id = ?", channelID).Delete(&ChannelCapabilitySnapshot{}).Error)
			capability := ChannelModelCapability{
				RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical",
				CatalogVersion: "integration-catalog", ChannelStatus: common.ChannelStatusEnabled,
				Priority: 10, ChannelType: 1, State: RouteCapabilityStateEligible,
			}
			initialFence := ChannelCapabilitySnapshotFence{}
			require.NoError(t, PublishChannelCapabilitySnapshot(context.Background(), channelID, initialFence, "integration-hash-1", "integration-catalog", []ChannelModelCapability{capability}))
			activeFence := ChannelCapabilitySnapshotFence{ActiveVersion: 1, SourceHash: "integration-hash-1", CatalogVersion: "integration-catalog"}
			require.NoError(t, PublishChannelCapabilitySnapshot(context.Background(), channelID, activeFence, "integration-hash-2", "integration-catalog", []ChannelModelCapability{capability}))
			active, err := FindActiveChannelCapabilities(context.Background(), []int{channelID}, "gpt-5", "openai")
			require.NoError(t, err)
			require.Len(t, active, 1)
			assert.Equal(t, int64(2), active[0].SnapshotVersion)
			_, err = FindChannelCapabilitySnapshotVersion(context.Background(), channelID, 1)
			require.NoError(t, err)
			assert.ErrorIs(t, PublishChannelCapabilitySnapshot(context.Background(), channelID, activeFence, "stale", "integration-catalog", []ChannelModelCapability{capability}), ErrCapabilitySnapshotConflict)

			hour := int64(1_700_000_000)
			deltas := []RouteShadowHourlyObservation{
				{HourStart: hour, InstanceID: "integration-a", Scope: RouteShadowObservationGlobal, ShadowDecisions: 2, EventAttempted: 2},
				{HourStart: hour, InstanceID: "integration-a", Scope: RouteShadowObservationModel, ModelName: "gpt-5", ShadowInitialDecisions: 2, CapabilityResolved: 2},
			}
			require.NoError(t, UpsertRouteShadowHourlyObservations(context.Background(), deltas))
			require.NoError(t, UpsertRouteShadowHourlyObservations(context.Background(), deltas))
			observations, err := ListRouteShadowHourlyObservations(context.Background(), hour, hour+3600)
			require.NoError(t, err)
			require.Len(t, observations, 2)
			byScope := make(map[string]RouteShadowHourlyObservation, len(observations))
			for _, observation := range observations {
				byScope[observation.Scope] = observation
			}
			assert.Equal(t, int64(4), byScope[RouteShadowObservationGlobal].ShadowDecisions)
			assert.Equal(t, int64(4), byScope[RouteShadowObservationModel].ShadowInitialDecisions)
		})
	}
}

func TestRoutingDatabaseIntegrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:routing-migration-sqlite?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	originalDB := DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
	})

	require.NoError(t, migrateRoutingModels(db))
	require.NoError(t, migrateChannelCapabilityIndexes())
	require.True(t, db.Migrator().HasIndex(&ChannelModelCapability{}, "channel_model_capability_snapshot"))
	require.True(t, db.Migrator().HasIndex(&ChannelRoutePolicy{}, "channel_route_policy_model"))
	require.True(t, db.Migrator().HasIndex(&RouteShadowHourlyObservation{}, "route_shadow_hourly_observation"))
	require.NoError(t, PublishChannelCapabilitySnapshot(context.Background(), 991002, ChannelCapabilitySnapshotFence{}, "sqlite-hash", "sqlite-catalog", []ChannelModelCapability{{
		RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical",
		ChannelStatus: common.ChannelStatusEnabled, State: RouteCapabilityStateEligible,
	}}))
	active, err := FindActiveChannelCapabilities(context.Background(), []int{991002}, "gpt-5", "openai")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, int64(1), active[0].SnapshotVersion)
}
