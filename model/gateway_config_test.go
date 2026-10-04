package model

import (
	"context"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayConfigEnforcesWaitAndUserQueueBoundsAtEverySource(t *testing.T) {
	config := DefaultGatewayConfig()
	require.NoError(t, config.Validate())
	config.WaitMS = 5001
	require.Error(t, config.Validate())
	config.WaitMS, config.UserQueueSize = 5000, 21
	require.Error(t, config.Validate())
	for _, override := range []GatewayAdmissionOverride{{WaitMS: common.GetPointer(5001)}, {QueueSize: common.GetPointer(21)}} {
		require.Error(t, override.Validate())
		config = DefaultGatewayConfig()
		config.Groups["test"] = override
		require.Error(t, config.Validate())
	}
	require.NoError(t, (GatewayAdmissionOverride{}).Validate())
	require.NoError(t, (GatewayAdmissionOverride{Concurrency: common.GetPointer(0), WaitMS: common.GetPointer(0), QueueSize: common.GetPointer(0)}).Validate())
}

func TestGatewayConfigPriceCeilingIsOptIn(t *testing.T) {
	config := DefaultGatewayConfig()
	// The default must not exclude any channel from automatic routing.
	limit, enabled, err := config.MaxRatioLimit()
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.True(t, limit.IsZero())

	config.MaxChannelRatio = "1.5"
	limit, enabled, err = config.MaxRatioLimit()
	require.NoError(t, err)
	require.True(t, enabled)
	assert.True(t, limit.Equal(decimal.RequireFromString("1.5")))
	require.NoError(t, config.Validate())

	for _, invalid := range []string{"0", "-1", "abc", "1e-30", "1001"} {
		config.MaxChannelRatio = invalid
		require.Error(t, config.Validate(), invalid)
		_, _, err = config.MaxRatioLimit()
		require.Error(t, err, invalid)
	}
}

func TestGatewayConfigUpgradePreservesExplicitValues(t *testing.T) {
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(backend.kind), func(t *testing.T) {
			if backend.env != "" && os.Getenv(backend.env) == "" {
				t.Skip(backend.env + " must target an isolated database service")
			}
			db, err := gatewayStartupDatabase(t, backend.kind, os.Getenv(backend.env))()
			require.NoError(t, err)
			previousDB := DB
			mainKind, logKind := common.MainDatabaseType(), common.LogDatabaseType()
			DB = db
			common.SetDatabaseTypes(backend.kind, backend.kind)
			initCol()
			t.Cleanup(func() {
				DB = previousDB
				common.SetDatabaseTypes(mainKind, logKind)
				initCol()
				sqlDB, err := db.DB()
				if assert.NoError(t, err) {
					assert.NoError(t, sqlDB.Close())
				}
			})
			require.NoError(t, db.AutoMigrate(&Option{}, &Token{}))
			for _, test := range []struct {
				name        string
				stored      string
				wantVersion int64
				wantQueue   int
				wantEnabled bool
			}{
				{"legacy_missing", `{"version":7,"user_concurrency":0,"wait_ms":0,"user_queue_size":0,"channel_queue_size":0}`, 8, 1024, true},
				{"partial_explicit_zero", `{"version":7,"user_concurrency":0,"wait_ms":0,"user_queue_size":0,"channel_queue_size":0,"total_queue_size":0,"retry_budget_enabled":false}`, 8, 0, false},
				{"already_current", `{"version":7,"user_concurrency":0,"wait_ms":0,"user_queue_size":0,"channel_queue_size":0,"total_queue_size":0,"retry_budget_enabled":false,"retry_budget_percent":10,"retry_budget_burst":20,"max_channel_ratio":""}`, 7, 0, false},
			} {
				t.Run(test.name, func(t *testing.T) {
					require.NoError(t, db.Where(map[string]any{"key": GatewayConfigKey}).Delete(&Option{}).Error)
					require.NoError(t, db.Create(&Option{Key: GatewayConfigKey, Value: test.stored}).Error)
					require.NoError(t, migrateGatewayDefaults(db))
					config, err := GetGatewayConfigContext(context.Background())
					require.NoError(t, err)
					assert.Equal(t, test.wantVersion, config.Version)
					assert.Zero(t, config.UserConcurrency)
					assert.Zero(t, config.WaitMS)
					assert.Zero(t, config.UserQueueSize)
					assert.Zero(t, config.ChannelQueueSize)
					assert.Equal(t, test.wantQueue, config.TotalQueueSize)
					assert.Equal(t, test.wantEnabled, config.RetryBudgetEnabled)
					assert.Equal(t, 10, config.RetryBudgetPercent)
					assert.Equal(t, 20, config.RetryBudgetBurst)
					var before, after Option
					require.NoError(t, db.Where(map[string]any{"key": GatewayConfigKey}).First(&before).Error)
					require.NoError(t, migrateGatewayDefaults(db))
					require.NoError(t, db.Where(map[string]any{"key": GatewayConfigKey}).First(&after).Error)
					assert.Equal(t, before.Value, after.Value, "second migration must not rewrite or increment the stored configuration")
					require.NoError(t, SaveGatewayConfig(config))
					assert.ErrorIs(t, SaveGatewayConfig(config), ErrGatewayConfigConflict)
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					_, err = GetGatewayConfigContext(ctx)
					assert.ErrorIs(t, err, context.Canceled)
				})
			}
		})
	}
}
