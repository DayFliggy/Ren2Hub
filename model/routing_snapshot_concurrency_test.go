package model

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCapabilityFirstPublishConcurrentDatabases(t *testing.T) {
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " must target an isolated database service")
			}
			var db *gorm.DB
			var err error
			if database.kind == common.DatabaseTypeSQLite {
				db, err = gorm.Open(sqlite.Open("file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "concurrent.db"))+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"), &gorm.Config{})
			} else {
				db, err = gatewayStartupDatabase(t, database.kind, dsn)()
			}
			require.NoError(t, err)
			previous, previousKind := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(database.kind)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			t.Cleanup(func() {
				DB = previous
				common.SetMainDatabaseType(previousKind)
				assert.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&ChannelCapabilitySnapshot{}, &ChannelModelCapability{}))
			for index, hashes := range [][2]string{{"same", "same"}, {"first", "second"}} {
				channelID := index + 1
				start, results := make(chan struct{}), make(chan error, 2)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				for _, hash := range hashes {
					go func(hash string) {
						<-start
						results <- PublishChannelCapabilitySnapshot(ctx, channelID, ChannelCapabilitySnapshotFence{}, hash, "catalog", []ChannelModelCapability{{
							RequestModel: "gpt-test", ActualModel: "gpt-test", State: RouteCapabilityStateEligible,
						}})
					}(hash)
				}
				close(start)
				successes := 0
				for range 2 {
					err := <-results
					if err == nil {
						successes++
					} else {
						assert.ErrorIs(t, err, ErrCapabilitySnapshotConflict)
					}
				}
				if hashes[0] == hashes[1] {
					assert.Equal(t, 2, successes, "identical startup projections must both succeed")
				} else {
					assert.Equal(t, 1, successes, "a stale, different projection must lose its fence")
				}
				var snapshot ChannelCapabilitySnapshot
				require.NoError(t, db.Where("channel_id = ?", channelID).First(&snapshot).Error)
				assert.Equal(t, int64(1), snapshot.ActiveVersion)
				var rows int64
				require.NoError(t, db.Model(&ChannelModelCapability{}).Where("channel_id = ?", channelID).Count(&rows).Error)
				assert.Equal(t, int64(1), rows)
			}
		})
	}
}
