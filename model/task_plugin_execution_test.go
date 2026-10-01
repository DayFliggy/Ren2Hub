package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withTaskPluginDatabases(t *testing.T, run func(*testing.T, *gorm.DB, func() (*gorm.DB, error))) {
	t.Helper()
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"},
	} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured; plugin database contract is unverified")
			}
			open := gatewayStartupDatabase(t, database.kind, dsn)
			db, err := open()
			require.NoError(t, err)
			previous, previousKind := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(database.kind)
			initCol()
			t.Cleanup(func() {
				DB = previous
				common.SetMainDatabaseType(previousKind)
				initCol()
				connection, err := db.DB()
				require.NoError(t, err)
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&TaskPlugin{}, &TaskPluginNamespace{}, &TaskPluginExecution{}, &BillingAttempt{}, &BillingRecovery{}, &Task{}))
			run(t, db, open)
		})
	}
}

func persistedPlugin(t *testing.T, version string) TaskPlugin {
	t.Helper()
	source := "export const version = '" + version + "';"
	plugin := TaskPlugin{Key: "fixture", Version: version, Source: LongText(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source))), Enabled: true, APIVersion: 1}
	require.NoError(t, SaveTaskPlugin(&plugin))
	return plugin
}

func TestTaskPluginReferencesSurviveActivationRestartAndDeletion(t *testing.T) {
	withTaskPluginDatabases(t, func(t *testing.T, db *gorm.DB, open func() (*gorm.DB, error)) {
		first := persistedPlugin(t, "1.0.0")
		second := persistedPlugin(t, "2.0.0")
		ref := TaskPluginRef{Key: first.Key, Version: first.Version, SourceHash: first.SourceHash}
		require.NoError(t, PinTaskPluginAttempt(context.Background(), "request", "attempt", ref))
		require.NoError(t, ActivateTaskPlugin(second.Key, second.Version))
		_, err := DeleteTaskPluginVersion(first.Key, first.Version)
		require.ErrorIs(t, err, ErrTaskPluginInUse)
		_, err = DeleteTaskPluginVersion(second.Key, second.Version)
		require.ErrorIs(t, err, ErrTaskPluginInUse, "active version never falls back implicitly")
		connection, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
		reopened, err := open()
		require.NoError(t, err)
		DB = reopened
		t.Cleanup(func() { conn, err := reopened.DB(); require.NoError(t, err); assert.NoError(t, conn.Close()) })
		loaded, err := GetTaskPluginExact(ref)
		require.NoError(t, err)
		assert.Equal(t, first.Source, loaded.Source)
		var pins int64
		require.NoError(t, reopened.Model(&TaskPluginExecution{}).Where("attempt_id = ?", "attempt").Count(&pins).Error)
		assert.EqualValues(t, 1, pins)
	})
}

func TestTaskPluginPinAndDeleteSerialize(t *testing.T) {
	withTaskPluginDatabases(t, func(t *testing.T, _ *gorm.DB, _ func() (*gorm.DB, error)) {
		first := persistedPlugin(t, "1.0.0")
		persistedPlugin(t, "2.0.0")
		require.NoError(t, ActivateTaskPlugin(first.Key, "2.0.0"))
		start := make(chan struct{})
		var group sync.WaitGroup
		var pinErr, deleteErr error
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			pinErr = PinTaskPluginAttempt(context.Background(), "request", "attempt", TaskPluginRef{Key: first.Key, Version: first.Version, SourceHash: first.SourceHash})
		}()
		go func() { defer group.Done(); <-start; _, deleteErr = DeleteTaskPluginVersion(first.Key, first.Version) }()
		close(start)
		group.Wait()
		require.False(t, pinErr == nil && deleteErr == nil, "a pinned executable cannot disappear")
		if pinErr == nil {
			require.ErrorIs(t, deleteErr, ErrTaskPluginInUse)
			_, err := GetTaskPluginExact(TaskPluginRef{Key: first.Key, Version: first.Version, SourceHash: first.SourceHash})
			require.NoError(t, err)
		}
		if deleteErr == nil {
			require.Error(t, pinErr)
		}
	})
}

func TestTaskPluginAttemptPinRollsBackSendingRecord(t *testing.T) {
	withTaskPluginDatabases(t, func(t *testing.T, db *gorm.DB, _ func() (*gorm.DB, error)) {
		plugin := persistedPlugin(t, "1.0.0")
		require.NoError(t, db.Create(&BillingRecovery{RequestID: "request", Status: BillingRecoveryStatusActive, PreConsumeState: BillingRecoveryPreConsumeCommitted, ConsumptionState: BillingRecoveryStateUnprepared, Source: "wallet"}).Error)
		version, err := billing.NewModelPriceVersion("fixture", billing.PricingValues{"ModelPrice": 1.0}, false)
		require.NoError(t, err)
		quote, err := billing.NewQuote(version, "1", nil, time.Now())
		require.NoError(t, err)
		ref := TaskPluginRef{Key: plugin.Key, Version: plugin.Version, SourceHash: fmt.Sprintf("%064d", 1)}
		require.Error(t, PrepareBillingAttemptWithPlugin(context.Background(), "request", "attempt", 1, quote, &ref))
		var attempts, pins int64
		require.NoError(t, db.Model(&BillingAttempt{}).Count(&attempts).Error)
		require.NoError(t, db.Model(&TaskPluginExecution{}).Count(&pins).Error)
		assert.Zero(t, attempts)
		assert.Zero(t, pins)
		ref.SourceHash = plugin.SourceHash
		require.NoError(t, PrepareBillingAttemptWithPlugin(context.Background(), "request", "attempt", 1, quote, &ref))
		require.NoError(t, PrepareBillingAttemptWithPlugin(context.Background(), "request", "attempt", 1, quote, &ref))
		require.NoError(t, db.Model(&BillingAttempt{}).Count(&attempts).Error)
		require.NoError(t, db.Model(&TaskPluginExecution{}).Count(&pins).Error)
		assert.EqualValues(t, 1, attempts)
		assert.EqualValues(t, 1, pins)
		modified := plugin
		modified.Source = "changed"
		modified.SourceHash = fmt.Sprintf("%x", sha256.Sum256([]byte(modified.Source)))
		require.Error(t, SaveTaskPlugin(&modified))
	})
}

func TestTaskPluginDeletionRequiresTerminalRecoveryAndRetainsArtifacts(t *testing.T) {
	for _, test := range []struct {
		name, status, task string
		allow              bool
	}{
		{"unfinished", BillingRecoveryStatusActive, "", false},
		{"released", BillingRecoveryStatusRefunded, "", true},
		{"settled_artifact", BillingRecoveryStatusSettled, "task-artifact", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			withTaskPluginDatabases(t, func(t *testing.T, db *gorm.DB, _ func() (*gorm.DB, error)) {
				first := persistedPlugin(t, "1.0.0")
				second := persistedPlugin(t, "2.0.0")
				require.NoError(t, PinTaskPluginAttempt(context.Background(), "request", "attempt", TaskPluginRef{Key: first.Key, Version: first.Version, SourceHash: first.SourceHash}))
				require.NoError(t, db.Create(&BillingRecovery{RequestID: "request", Status: test.status}).Error)
				if test.task != "" {
					require.NoError(t, db.Model(&TaskPluginExecution{}).Where("attempt_id = ?", "attempt").Update("task_id", test.task).Error)
				}
				require.NoError(t, ActivateTaskPlugin(second.Key, second.Version))
				_, err := DeleteTaskPluginVersion(first.Key, first.Version)
				if test.allow {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, ErrTaskPluginInUse)
				}
			})
		})
	}
}

func TestTaskPluginSourceDialectRoundTrip(t *testing.T) {
	withTaskPluginDatabases(t, func(t *testing.T, db *gorm.DB, _ func() (*gorm.DB, error)) {
		plugin := TaskPlugin{Key: "large-source", Version: "1.0.0", Enabled: true, APIVersion: 1, Source: LongText(strings.Repeat("x", 8<<20))}
		plugin.SourceHash = fmt.Sprintf("%x", sha256.Sum256([]byte(plugin.Source)))
		require.NoError(t, SaveTaskPlugin(&plugin))
		loaded, err := GetTaskPluginExact(TaskPluginRef{Key: plugin.Key, Version: plugin.Version, SourceHash: plugin.SourceHash})
		require.NoError(t, err)
		assert.Equal(t, plugin.Source, loaded.Source)
		snapshot, err := GetTaskPluginSyncSnapshot()
		require.NoError(t, err)
		require.Len(t, snapshot.Plugins, 1)
		assert.Empty(t, snapshot.Plugins[0].Source)
		require.NoError(t, db.AutoMigrate(&TaskPlugin{}))
	})
}
