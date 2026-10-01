package model

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useOpsTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)), &gorm.Config{})
	require.NoError(t, err)
	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		sqlDB, _ := db.DB()
		assert.NoError(t, sqlDB.Close())
	})
	return db
}

func TestDropRetiredSystemInstancesDiscardsTheHeartbeatRegistry(t *testing.T) {
	db := useOpsTestDB(t, "retire.db")
	require.NoError(t, db.Exec("CREATE TABLE system_instances (node_name varchar(128) PRIMARY KEY, info text)").Error)
	require.NoError(t, db.Exec("INSERT INTO system_instances (node_name, info) VALUES ('old-node', '{}')").Error)
	require.NoError(t, dropRetiredSystemInstances(db))
	assert.False(t, db.Migrator().HasTable("system_instances"))
	require.NoError(t, dropRetiredSystemInstances(db), "fresh and already cleaned databases are unaffected")
}

func TestReadOpsChannelHealthGroupsBreakersWithoutExposingKeyScopes(t *testing.T) {
	db := useOpsTestDB(t, "ops-health.db")
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelHealth{}))
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1, Name: "primary", Status: common.ChannelStatusEnabled, Key: "k1"},
		{Id: 2, Name: "backup", Status: common.ChannelStatusAutoDisabled, Key: "k2"},
		{Id: 3, Name: "manual", Status: common.ChannelStatusManuallyDisabled, Key: "k3"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelHealth{
		{ChannelID: 1, Model: "gpt-4o", KeyScope: "scope-a", State: RouteHealthStateHalfOpen, FailureCount: 2, CooldownUntil: 100, LastLatencyMS: 900, UpdatedAt: 50},
		{ChannelID: 1, Model: "gpt-4o", KeyScope: "scope-b", State: RouteHealthStateOpen, FailureCount: 5, CooldownUntil: 300, LastLatencyMS: 1200, UpdatedAt: 40},
		{ChannelID: 2, Model: "claude", KeyScope: "scope-c", State: RouteHealthStateOpen, FailureCount: 1, UpdatedAt: 60},
		{ChannelID: 1, Model: "gpt-4o-mini", KeyScope: "scope-a", State: RouteHealthStateClosed, UpdatedAt: 70},
	}).Error)

	health, err := ReadOpsChannelHealth(1)
	require.NoError(t, err)
	assert.Equal(t, OpsChannelCounts{Total: 3, Enabled: 1, ManuallyDisabled: 1, AutoDisabled: 1}, health.Channels)
	assert.Equal(t, OpsBreakerCounts{Closed: 1, Open: 2, HalfOpen: 1}, health.Breakers)
	require.Len(t, health.Unhealthy, 1, "the limit counts channel and model groups")
	assert.Equal(t, "backup", health.Unhealthy[0].ChannelName, "the most recently updated group comes first")

	health, err = ReadOpsChannelHealth(10)
	require.NoError(t, err)
	require.Len(t, health.Unhealthy, 2)
	latency := int64(900)
	assert.Equal(t, OpsUnhealthyRoute{ChannelID: 1, ChannelName: "primary", Model: "gpt-4o", State: RouteHealthStateOpen,
		FailureCount: 5, CooldownUntil: 300, LastLatencyMS: &latency, UpdatedAt: 50}, health.Unhealthy[1],
		"the worst state and counts win; latency comes from the newest row; unmeasured TTFT is null")
	assert.Nil(t, health.Unhealthy[0].LastLatencyMS, "0 means not measured, never a 0 ms reading")
	encoded, err := common.Marshal(health)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "scope-", "credential key scopes stay on the server")
}

func TestSummarizeSystemTaskActivityKeepsTheLatestOutcomePerType(t *testing.T) {
	db := useOpsTestDB(t, "task-activity.db")
	require.NoError(t, db.AutoMigrate(&SystemTask{}))
	tasks := []SystemTask{
		{TaskID: "a1", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusSucceeded, UpdatedAt: 100},
		{TaskID: "a2", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusFailed, Error: "older failure", UpdatedAt: 150},
		{TaskID: "a3", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusFailed, Error: "latest failure", UpdatedAt: 200},
		{TaskID: "b1", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusRunning, UpdatedAt: 300},
		{TaskID: "c1", Type: "not_requested", Status: SystemTaskStatusSucceeded, UpdatedAt: 400},
	}
	require.NoError(t, db.Create(&tasks).Error)
	activity, err := SummarizeSystemTaskActivity([]string{SystemTaskTypeChannelTest, SystemTaskTypeModelUpdate, SystemTaskTypeLogCleanup})
	require.NoError(t, err)
	assert.Equal(t, SystemTaskActivity{LastSuccessAt: 100, LastFailureAt: 200, LastSuccessID: tasks[0].ID, LastFailureID: tasks[2].ID, LastError: "latest failure"}, activity[SystemTaskTypeChannelTest])
	assert.Equal(t, SystemTaskActivity{Active: true}, activity[SystemTaskTypeModelUpdate])
	assert.NotContains(t, activity, SystemTaskTypeLogCleanup, "a type that never ran has no activity")
	assert.NotContains(t, activity, "not_requested")
}

func TestReadOpsChannelHealthLimitsRoutesAfterGroupingCredentialScopes(t *testing.T) {
	db := useOpsTestDB(t, "ops-health-many-keys.db")
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelHealth{}))
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1, Name: "rotating-keys", Status: common.ChannelStatusEnabled, Key: "test-key"},
		{Id: 2, Name: "backup", Status: common.ChannelStatusEnabled, Key: "backup-key"},
	}).Error)
	rows := make([]ChannelHealth, 0, 502)
	for i := range 500 {
		rows = append(rows, ChannelHealth{
			ChannelID: 1, Model: "model-a", KeyScope: fmt.Sprintf("key-%d", i), State: RouteHealthStateHalfOpen,
			FailureCount: 1, UpdatedAt: int64(1000 + i), LastLatencyMS: int64(100 + i),
		})
	}
	rows = append(rows,
		ChannelHealth{ChannelID: 1, Model: "model-a", KeyScope: "still-open", State: RouteHealthStateOpen, FailureCount: 8, CooldownUntil: 2000, UpdatedAt: 900},
		ChannelHealth{ChannelID: 2, Model: "model-b", KeyScope: "backup", State: RouteHealthStateOpen, FailureCount: 2, UpdatedAt: 800},
	)
	require.NoError(t, db.CreateInBatches(rows, 100).Error)

	health, err := ReadOpsChannelHealth(2)
	require.NoError(t, err)
	require.Len(t, health.Unhealthy, 2, "credential count must not hide another requested route")
	primary := health.Unhealthy[0]
	assert.Equal(t, RouteHealthStateOpen, primary.State, "an older open credential remains worse than a newer half-open credential")
	assert.Equal(t, 8, primary.FailureCount)
	assert.Equal(t, int64(2000), primary.CooldownUntil)
	require.NotNil(t, primary.LastLatencyMS)
	assert.Equal(t, int64(599), *primary.LastLatencyMS, "latency still comes from the latest observation")
	assert.Equal(t, "backup", health.Unhealthy[1].ChannelName)
}

func TestReadOpsChannelHealthExcludesDeletedChannels(t *testing.T) {
	db := useOpsTestDB(t, "ops-health-deleted.db")
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelHealth{}, &Ability{}))
	channel := Channel{Id: 1, Name: "retired", Status: common.ChannelStatusEnabled, Key: "test-key"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&ChannelHealth{
		ChannelID: channel.Id, Model: "model-a", KeyScope: "failed-key", State: RouteHealthStateOpen, FailureCount: 3,
	}).Error)
	health, err := ReadOpsChannelHealth(10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, health.Breakers.Open)
	require.Len(t, health.Unhealthy, 1)

	require.NoError(t, channel.Delete())
	health, err = ReadOpsChannelHealth(10)
	require.NoError(t, err)
	assert.Zero(t, health.Channels.Total)
	assert.Zero(t, health.Breakers.Open, "retained historical health rows must not count as current breakers")
	assert.Empty(t, health.Unhealthy, "a removed channel cannot be an unhealthy route")
}
