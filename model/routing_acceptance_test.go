package model

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type acceptanceOldAbility struct {
	Group     string `gorm:"primaryKey;size:64"`
	Model     string `gorm:"primaryKey;size:191"`
	ChannelID int    `gorm:"primaryKey;autoIncrement:false"`
	Enabled   bool
	Priority  int64
	Weight    int
	Tag       string
}

func (acceptanceOldAbility) TableName() string { return "abilities" }

// This entry point is intentionally separate from production-hour tests. The
// runner creates a fresh container database and supplies this explicit marker.
func TestRoutingAcceptanceDatabase(t *testing.T) {
	if os.Getenv("REN2HUB_ISOLATED_ACCEPTANCE") != "synthetic-empty-database" {
		t.Skip("isolated database runner only")
	}
	previousSQLitePath := common.SQLitePath
	common.SQLitePath = t.TempDir() + "/acceptance.db"
	t.Cleanup(func() { common.SQLitePath = previousSQLitePath })
	db, kind, err := chooseDB("REN2HUB_ACCEPTANCE_DSN", false)
	require.NoError(t, err)
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "acceptance refuses a nonempty database")
	sqlDB.SetMaxOpenConns(4)
	previousDB, previousLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(kind, kind)
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		initCol()
	})
	var version string
	switch kind {
	case common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL:
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	default:
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	}
	t.Logf("database=%s version=%s", kind, version)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Channel{}, &Task{}, &Log{}, &QuotaData{}, &PerfMetric{},
		&UserRouteProfile{}, &SubscriptionPlan{}, &UserSubscription{}, &Option{}, &acceptanceOldAbility{}))
	require.NoError(t, db.Migrator().RenameColumn(&User{}, "account_class", "group"))
	require.NoError(t, db.Exec("ALTER TABLE tokens ADD COLUMN auto_groups TEXT").Error)
	require.NoError(t, db.Table("users").Create(map[string]any{"id": 1, "username": "fixture", "password": "unused", "group": "vip", "quota": 12345, "auth_version": 9, "used_quota": 300}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1, Key: "synthetic-only", Models: "gpt-test", Status: common.ChannelStatusEnabled, UsedQuota: 300}).Error)
	require.NoError(t, db.Create(&[]acceptanceOldAbility{
		{Group: "vip", Model: "gpt-test", ChannelID: 1, Enabled: true},
		{Group: "default", Model: "gpt-test", ChannelID: 1, Enabled: false},
		{Group: "", Model: "hidden", ChannelID: 1, Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&Option{Key: "GroupRatio", Value: `{"vip":9,"unused":11}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "TopupGroupRatio", Value: `{"vip":1.2,"unused":0}`}).Error)
	require.NoError(t, db.Table("tasks").Create(map[string]any{"id": 1, "status": TaskStatusSubmitted, "quota": 300, "user_id": 1, "channel_id": 1,
		"private_data": `{"vendor":{"kept":true},"billing_source":"wallet","billing_context":{"group_ratio":0,"model_ratio":2}}`}).Error)
	ctx := context.Background()
	before, err := InspectRoutingMigration(ctx, db, "main")
	require.NoError(t, err)
	require.Empty(t, before.Blockers)
	after, err := ApplyRoutingMigration(ctx, db, "main")
	require.NoError(t, err, "%+v", after)
	assert.Equal(t, before.Accounting, after.Accounting)
	assert.Equal(t, before.CandidateDigest, after.CandidateDigest)
	assert.Empty(t, after.RetiredColumns)
	_, err = ResumeRoutingMigration(ctx, db, "main")
	require.NoError(t, err)
	_, err = VerifyRoutingMigration(ctx, db, "main")
	require.NoError(t, err)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, "vip", user.AccountClass)
	assert.Equal(t, int64(9), user.AuthVersion)
	assert.Equal(t, 12345, user.Quota)
	var raw string
	require.NoError(t, db.Table("tasks").Where("id = ?", 1).Pluck("private_data", &raw).Error)
	assert.JSONEq(t, `{"vendor":{"kept":true},"billing_source":"wallet","billing_context":{"billing_multiplier":0,"multiplier_source":"historical_snapshot","model_ratio":2}}`, raw)
	require.NoError(t, db.AutoMigrate(&TaskSettlement{}, &TaskSettlementLogReceipt{}, &FlushOperation{}))
	var task Task
	require.NoError(t, db.First(&task, 1).Error)
	for range 2 {
		receipt, err := ApplyTaskSettlement(ctx, &task, 0, true, RecordTaskBillingLogParams{UserId: 1, ChannelId: 1, ModelName: "gpt-test"})
		require.NoError(t, err)
		assert.True(t, receipt.Applied)
		require.NoError(t, DeliverTaskSettlementLog(ctx, task.ID))
	}
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 12645, user.Quota)
	var count int64
	require.NoError(t, db.Model(&Log{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	fmt.Printf("ROUTING_DATABASE_ACCEPTANCE_OK database=%s\n", kind)
}
