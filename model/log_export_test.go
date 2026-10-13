package model

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStreamLogsStopsWhenConsumerFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	oldDB, oldLogDB, oldType := DB, LOG_DB, common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetLogDatabaseType(oldType)
		assert.NoError(t, conn.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}, &Channel{}))
	require.NoError(t, db.Create(&[]Log{{UserId: 1, Type: LogTypeConsume}, {UserId: 1, Type: LogTypeConsume}}).Error)
	failed := errors.New("consumer disconnected")
	count := 0
	err = StreamLogs(context.Background(), 1, false, LogFilter{}, func(*Log) error { count++; return failed })
	assert.ErrorIs(t, err, failed)
	assert.Equal(t, 1, count)
	var rows int64
	require.NoError(t, db.Model(&Log{}).Count(&rows).Error)
	assert.Equal(t, int64(2), rows)
}

func TestLogExportFiltersAcrossDatabases(t *testing.T) {
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"}} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured; export contract unverified")
			}
			db, err := gatewayStartupDatabase(t, database.kind, dsn)()
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			oldDB, oldLogDB, oldMain, oldLog := DB, LOG_DB, common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(database.kind, database.kind)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLogDB
				common.SetDatabaseTypes(oldMain, oldLog)
				initCol()
				assert.NoError(t, conn.Close())
			})
			require.NoError(t, db.AutoMigrate(&Log{}, &Channel{}))
			channel := Channel{Name: "export fixture"}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&[]Log{
				{UserId: 21, Type: LogTypeConsume, CreatedAt: 100, ModelName: "model_exact", Group: "old-team", ChannelId: channel.Id, Quota: 9007199254740993, RequestId: "selected"},
				{UserId: 22, Type: LogTypeConsume, CreatedAt: 101, ModelName: "model_exact", Group: "old-team", RequestId: "other-user"},
				{UserId: 21, Type: LogTypeManage, CreatedAt: 102, ModelName: "model_exact", Group: "old-team", RequestId: "operation"},
				{UserId: 21, Type: LogTypeConsume, CreatedAt: 99, ModelName: "model_exact", Group: "old-team", RequestId: "outside-time"},
				{UserId: 21, Type: LogTypeConsume, CreatedAt: 100, ModelName: "modelXexact", Group: "old-team", RequestId: "wrong-model"},
			}).Error)
			filter := LogFilter{StartTimestamp: 100, EndTimestamp: 102, ModelName: "model_exact", Group: "old-team"}
			var rows []*Log
			require.NoError(t, StreamLogs(context.Background(), 21, false, filter, func(row *Log) error { rows = append(rows, row); return nil }))
			require.Len(t, rows, 1)
			assert.Equal(t, "selected", rows[0].RequestId)
			assert.Equal(t, common.Quota(9007199254740993), rows[0].Quota)
			assert.Equal(t, channel.Name, rows[0].ChannelName)
			listed, total, err := GetUserLogs(21, 0, 100, 102, "model_exact", "", 0, 10, "old-team", "", "", "")
			require.NoError(t, err)
			require.Equal(t, int64(1), total)
			assert.Equal(t, listed, rows)
			filter.Type = LogTypeConsume
			rows = nil
			require.NoError(t, StreamLogs(context.Background(), 21, true, filter, func(row *Log) error { rows = append(rows, row); return nil }))
			require.Len(t, rows, 2)
			assert.Equal(t, []string{"other-user", "selected"}, []string{rows[0].RequestId, rows[1].RequestId})
		})
	}
}
