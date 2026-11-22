package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

// The old adapter called the administrator's wildcard search. This adapter
// exercises the actual self-service ID boundary, retaining the correct assertion.
func restoredControlSelfStat(_ context.Context) (Stat, error) {
	return SumUsedQuotaForUser(1, LogTypeConsume, 0, 0, "", "", 0)
}

func selfStatisticsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "self-stat.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldLog, oldMainType, oldLogType := LOG_DB, common.MainDatabaseType(), common.LogDatabaseType()
	LOG_DB = db
	common.SetDatabaseTypes(oldMainType, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = oldLog
		common.SetDatabaseTypes(oldMainType, oldLogType)
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return db
}

func TestRestoredNativeControlSelfStatIdentity(t *testing.T) {
	db := selfStatisticsDB(t)
	require.NoError(t, db.Create(&[]Log{{UserId: 1, Username: "probe%", Type: LogTypeConsume, Quota: 10, CreatedAt: time.Now().Unix()}, {UserId: 2, Username: "probe-other", Type: LogTypeConsume, Quota: 90, CreatedAt: time.Now().Unix()}}).Error)
	stat, err := restoredControlSelfStat(context.Background())
	require.NoError(t, err)
	if stat.Quota != 10 {
		t.Errorf("CONTROL_SELF_STAT: unrelated user quota included: %d", stat.Quota)
	}
}
