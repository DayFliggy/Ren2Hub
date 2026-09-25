package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemTaskIdenticalStateKeepsLeaseAcrossDatabases(t *testing.T) {
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"}} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured")
			}
			db, err := gatewayStartupDatabase(t, database.kind, dsn)()
			require.NoError(t, err)
			previous := DB
			DB = db
			t.Cleanup(func() {
				DB = previous
				connection, err := db.DB()
				require.NoError(t, err)
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&SystemTask{}, &SystemTaskLock{}))
			task, err := CreateSystemTask(SystemTaskTypeLogCleanup, nil, nil)
			require.NoError(t, err)
			_, claimed, err := ClaimSystemTask(task.ID, task.Type, "owner", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, claimed)
			state := testSystemTaskState{Total: 10, Processed: 10, Progress: 100}
			require.NoError(t, UpdateSystemTaskState(task.TaskID, "owner", state))
			require.NoError(t, UpdateSystemTaskState(task.TaskID, "owner", state))
			require.ErrorIs(t, UpdateSystemTaskState(task.TaskID, "wrong-owner", state), ErrSystemTaskLockLost)
			require.NoError(t, FinishSystemTask(task.TaskID, "owner", SystemTaskStatusSucceeded, nil, ""))
			require.ErrorIs(t, UpdateSystemTaskState(task.TaskID, "owner", state), ErrSystemTaskLockLost)
		})
	}
}
