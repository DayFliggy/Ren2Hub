package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newRoutingMigrationFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/migration.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Channel{}, &Task{}, &Log{}, &QuotaData{}, &PerfMetric{}, &UserRouteProfile{}, &SubscriptionPlan{}, &UserSubscription{}, &Option{}))
	require.NoError(t, db.Migrator().RenameColumn(&User{}, "account_class", "group"))
	require.NoError(t, db.Exec("ALTER TABLE tokens ADD COLUMN auto_groups TEXT").Error)
	require.NoError(t, db.Exec("ALTER TABLE channels ADD COLUMN `group` TEXT").Error)
	require.NoError(t, db.Exec("ALTER TABLE tasks ADD COLUMN `group` TEXT").Error)
	require.NoError(t, db.Exec("ALTER TABLE logs ADD COLUMN `group` TEXT").Error)
	require.NoError(t, db.Exec("CREATE TABLE abilities (`group` TEXT, model TEXT, channel_id INTEGER, enabled INTEGER, priority INTEGER, weight INTEGER, tag TEXT, PRIMARY KEY (`group`,model,channel_id))").Error)
	require.NoError(t, db.Table("users").Create(map[string]any{"id": 1, "username": "fixture", "password": "fixture", "group": "vip", "quota": 12345, "auth_version": 9}).Error)
	require.NoError(t, db.Create(&Channel{Id: 1, Key: "fixture", Models: "gpt-test", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Exec("INSERT INTO abilities (`group`,model,channel_id,enabled,priority,weight) VALUES ('vip','gpt-test',1,1,0,0),('default','gpt-test',1,0,0,0),('','hidden',1,1,0,0)").Error)
	require.NoError(t, db.Create(&Option{Key: "GroupRatio", Value: `{"vip":9,"unused":11}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "TopupGroupRatio", Value: `{"vip":1.2,"unused":0}`}).Error)
	require.NoError(t, db.Table("tasks").Create(map[string]any{"id": 1, "status": TaskStatusSubmitted, "quota": 100, "private_data": `{"vendor":{"kept":true},"billing_context":{"group_ratio":0,"model_ratio":2}}`}).Error)
	return db
}

func TestOfflineMigrationPreservesAccountsCapabilitiesAndFrozenPrices(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	ctx := context.Background()
	before, err := InspectRoutingMigration(ctx, db, "main")
	require.NoError(t, err)
	require.Empty(t, before.Blockers)
	require.Error(t, requireRoutingSchema(db, "main"))
	first, err := ApplyRoutingMigration(ctx, db, "main")
	require.NoError(t, err, "%+v", first)
	assert.Equal(t, before.Accounting, first.Accounting)
	assert.Empty(t, first.RetiredColumns)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, "vip", user.AccountClass)
	assert.Equal(t, 12345, user.Quota)
	assert.Equal(t, int64(9), user.AuthVersion)
	var classes []AccountClass
	require.NoError(t, db.Order("code ASC").Find(&classes).Error)
	require.Len(t, classes, 3)
	assert.Equal(t, 1.0, classes[1].TopupMultiplier)
	assert.Equal(t, 1.2, classes[2].TopupMultiplier)
	var abilities []Ability
	require.NoError(t, db.Order("model ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.True(t, abilities[0].Enabled)
	assert.False(t, abilities[1].Enabled)
	var raw string
	require.NoError(t, db.Table("tasks").Where("id = ?", 1).Pluck("private_data", &raw).Error)
	assert.JSONEq(t, `{"vendor":{"kept":true},"billing_context":{"billing_multiplier":0,"multiplier_source":"historical_snapshot","model_ratio":2}}`, raw)
	repeated, err := ApplyRoutingMigration(ctx, db, "main")
	require.NoError(t, err)
	assert.Equal(t, first.Accounting, repeated.Accounting)
	assert.Equal(t, before.Fingerprint, repeated.SourceFingerprint)
	var state RoutingSchemaState
	require.NoError(t, db.First(&state, "role = ?", "main").Error)
	assert.Equal(t, before.Accounting, first.Accounting)
	assert.NotEmpty(t, state.BaselineAccounting)
	assert.Equal(t, before.CandidateDigest, state.CandidateDigest)
	assert.Equal(t, RoutingMigrationToolVersion, state.ToolVersion)
}

func TestOfflineMigrationResumesCommittedJSONBatches(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	for id := 1; id <= 401; id++ {
		require.NoError(t, db.Table("logs").Create(map[string]any{"id": id, "quota": 3, "other": `{"group_ratio":2,"unknown":{"number":9007199254740993}}`}).Error)
	}
	injected := errors.New("injected batch interruption")
	updated := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:interrupt_json", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			updated++
			if updated == 202 {
				tx.AddError(injected)
			}
		}
	}))
	_, err := ApplyRoutingMigration(context.Background(), db, "main")
	require.ErrorIs(t, err, injected)
	require.NoError(t, db.Callback().Update().Remove("test:interrupt_json"))
	var checkpoint RoutingSchemaState
	require.NoError(t, db.First(&checkpoint, "role = ?", "main").Error)
	assert.Equal(t, "json_logs", checkpoint.Stage)
	assert.Equal(t, int64(200), checkpoint.Cursor)
	require.Error(t, requireRoutingSchema(db, "main"), "unfinished migration must block application startup")
	var unconverted int64
	require.NoError(t, db.Table("logs").Where("other LIKE ?", "%group_ratio%").Count(&unconverted).Error)
	assert.Equal(t, int64(201), unconverted, "the interrupted chunk must roll back with its cursor")
	assert.True(t, db.Migrator().HasColumn("tokens", "auto_groups"), "no physical retirement before JSON verification")
	report, err := ResumeRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	assert.Equal(t, "complete", report.Stage)
	assert.Equal(t, "1203", report.Accounting["logs.quota"])
	assert.Equal(t, checkpoint.SourceFingerprint, report.SourceFingerprint)
	var tail string
	require.NoError(t, db.Table("logs").Where("id = ?", 401).Pluck("other", &tail).Error)
	assert.Contains(t, tail, "9007199254740993")
	assert.NotContains(t, tail, "group_ratio")
}

func TestOfflineMigrationResumesAbilityDDLWindows(t *testing.T) {
	for _, stage := range []string{"copying", "copied", "source_renamed", "activated", "complete"} {
		t.Run(stage, func(t *testing.T) {
			db := newRoutingMigrationFixture(t)
			injected := errors.New("injected ability checkpoint interruption")
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:interrupt_ability", func(tx *gorm.DB) {
				state, ok := tx.Statement.Dest.(*RoutingSchemaState)
				if ok && state.AbilityStep == stage {
					tx.AddError(injected)
				}
			}))
			_, err := ApplyRoutingMigration(context.Background(), db, "main")
			require.ErrorIs(t, err, injected)
			require.NoError(t, db.Callback().Update().Remove("test:interrupt_ability"))
			report, err := ResumeRoutingMigration(context.Background(), db, "main")
			require.NoError(t, err)
			assert.Equal(t, int64(2), report.CandidateCount)
			assert.Empty(t, report.RetiredColumns)
			var abilities []Ability
			require.NoError(t, db.Order("model ASC").Find(&abilities).Error)
			require.Len(t, abilities, 2)
			assert.True(t, abilities[0].Enabled)
			assert.False(t, abilities[1].Enabled)
			tables, err := db.Migrator().GetTables()
			require.NoError(t, err)
			for _, table := range tables {
				assert.False(t, strings.HasPrefix(table, "routing_migration_"), table)
			}
		})
	}
}

func TestOfflineMigrationRejectsChangedEvidence(t *testing.T) {
	for _, mutation := range []string{"database_identity", "tool_version", "accounting", "candidate", "json", "option", "retired_index", "zero_row", "account_swap"} {
		t.Run(mutation, func(t *testing.T) {
			db := newRoutingMigrationFixture(t)
			if mutation == "account_swap" {
				require.NoError(t, db.Table("users").Create(map[string]any{"id": 2, "username": "fixture2", "password": "fixture", "group": "vip", "quota": 7, "auth_version": 9}).Error)
			}
			require.NoError(t, db.Exec("CREATE INDEX idx_old_token_group ON tokens (auto_groups)").Error)
			_, err := ApplyRoutingMigration(context.Background(), db, "main")
			require.NoError(t, err)
			switch mutation {
			case "database_identity", "tool_version":
				require.NoError(t, db.Model(&RoutingSchemaState{}).Where("role = ?", "main").Update(mutation, "changed").Error)
			case "accounting":
				require.NoError(t, db.Table("users").Where("id = 1").Update("quota", 12346).Error)
			case "candidate":
				require.NoError(t, db.Table("abilities").Where("model = ?", "hidden").Update("enabled", true).Error)
			case "json":
				require.NoError(t, db.Table("tasks").Where("id = 1").Update("private_data", `{"billing_context":{"group_ratio":0}}`).Error)
			case "option":
				require.NoError(t, db.Create(&Option{Key: "GroupRatio", Value: `{}`}).Error)
			case "retired_index":
				require.NoError(t, db.Exec("CREATE INDEX idx_old_token_group ON tokens (remain_quota)").Error)
			case "zero_row":
				require.NoError(t, db.Create(&Log{Id: 2, Quota: 0}).Error)
			case "account_swap":
				require.NoError(t, db.Table("users").Where("id = 1").Update("quota", 7).Error)
				require.NoError(t, db.Table("users").Where("id = 2").Update("quota", 12345).Error)
			}
			_, err = VerifyRoutingMigration(context.Background(), db, "main")
			require.Error(t, err, "verify must reject changed %s evidence", mutation)
		})
	}
}

func TestOfflineMigrationDoesNotDeleteUnownedCopyTables(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	require.NoError(t, db.Exec("CREATE TABLE routing_migration_abilities (evidence TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO routing_migration_abilities VALUES ('retain')").Error)
	_, err := ApplyRoutingMigration(context.Background(), db, "main")
	require.ErrorContains(t, err, "unowned migration table")
	var evidence string
	require.NoError(t, db.Table("routing_migration_abilities").Pluck("evidence", &evidence).Error)
	assert.Equal(t, "retain", evidence)
	assert.False(t, db.Migrator().HasTable(&RoutingSchemaState{}))
}

func TestOfflineMigrationRejectsUnknownSourceColumns(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	require.NoError(t, db.Exec("ALTER TABLE tokens ADD COLUMN unknown_route_contract TEXT").Error)
	report, err := InspectRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	assert.Contains(t, report.Blockers, "unsupported baseline column: tokens.unknown_route_contract")
	_, err = ApplyRoutingMigration(context.Background(), db, "main")
	require.Error(t, err)
	assert.False(t, db.Migrator().HasTable(&RoutingSchemaState{}))
}

func TestOfflineMigrationReportsRulePathsWithoutValues(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	privateValue := "not-a-real-production-value"
	raw := fmt.Sprintf(`{"operations":[{"mode":"set","path":"model","value":%q,"conditions":[{"path":"using_group","value":"vip"}]}]}`, privateValue)
	require.NoError(t, db.Table("channels").Where("id = 1").Update("param_override", raw).Error)
	report, err := InspectRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	require.Len(t, report.Blockers, 1)
	assert.Contains(t, report.Blockers[0], "$.operations[0].conditions[0].path")
	assert.NotContains(t, report.Blockers[0], privateValue)
	require.NoError(t, db.Table("channels").Where("id = 1").Update("param_override", `{"model":"using_group is a literal SDK value"}`).Error)
	report, err = InspectRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	assert.Empty(t, report.Blockers)
}

func TestOfflineMigrationConvertsOnlyInactiveAffinityFlag(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	rules := `[{"name":"fixture","include_using_group":false,"unknown":9007199254740993,"key_sources":[{"type":"request_header","key":"user_group"}]}]`
	require.NoError(t, db.Create(&Option{Key: "channel_affinity_setting.rules", Value: rules}).Error)
	report, err := InspectRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	assert.Empty(t, report.Blockers)
	_, err = ApplyRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	var option Option
	require.NoError(t, db.First(&option, "key = ?", "channel_affinity_setting.rules").Error)
	assert.NotContains(t, option.Value, "include_using_group")
	assert.Contains(t, option.Value, "9007199254740993")
	assert.Contains(t, option.Value, "user_group")
	require.NoError(t, db.Model(&Option{}).Where("key = ?", option.Key).Update("value", `[{"name":"fixture","include_using_group":true}]`).Error)
	report, err = InspectRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	require.Len(t, report.Blockers, 1)
	assert.Contains(t, report.Blockers[0], "$[0].include_using_group")
}

func TestOfflineMigrationPreservesSummaryTotalsAndIndexes(t *testing.T) {
	db := newRoutingMigrationFixture(t)
	require.NoError(t, db.Migrator().DropTable(&PerfMetric{}))
	require.NoError(t, db.Exec("CREATE TABLE perf_metrics (id INTEGER PRIMARY KEY, model_name TEXT, `group` TEXT, bucket_ts INTEGER, request_count INTEGER, success_count INTEGER, total_latency_ms INTEGER, ttft_sum_ms INTEGER, ttft_count INTEGER, output_tokens INTEGER, generation_ms INTEGER)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_perf_model_group_bucket ON perf_metrics (model_name, `group`, bucket_ts)").Error)
	require.NoError(t, db.Exec("INSERT INTO perf_metrics VALUES (7,'gpt-test','vip',1000,2,1,100,20,1,10,50), (12,'gpt-test','default',1000,3,3,210,60,2,20,90)").Error)
	require.NoError(t, db.Exec("ALTER TABLE quota_data ADD COLUMN use_group TEXT").Error)
	for id, group := range []string{"vip", ""} {
		require.NoError(t, db.Table("quota_data").Create(map[string]any{"id": id + 1, "user_id": 1, "username": "fixture", "model_name": "gpt-test", "created_at": 1000, "use_group": group, "count": 2, "quota": 31, "token_used": 13}).Error)
	}
	report, err := ApplyRoutingMigration(context.Background(), db, "main")
	require.NoError(t, err)
	assert.Equal(t, "5", report.Accounting["perf_metrics.request_count"])
	assert.Equal(t, "310", report.Accounting["perf_metrics.total_latency_ms"])
	assert.Equal(t, "62", report.Accounting["quota_data.quota"])
	var performance []PerfMetric
	require.NoError(t, db.Find(&performance).Error)
	require.Len(t, performance, 1)
	assert.Equal(t, int64(5), performance[0].RequestCount)
	assert.Equal(t, int64(310), performance[0].TotalLatencyMs)
	assert.False(t, db.Migrator().HasIndex("perf_metrics", "idx_perf_model_group_bucket"))
	assert.True(t, db.Migrator().HasIndex("perf_metrics", "idx_perf_model_bucket"))
	var quotaRows []QuotaData
	require.NoError(t, db.Find(&quotaRows).Error)
	require.Len(t, quotaRows, 1)
	assert.Equal(t, 62, quotaRows[0].Quota)
	assert.Equal(t, 26, quotaRows[0].TokenUsed)
}

func TestOfflineMigrationEmptyAndIndependentLogFailure(t *testing.T) {
	newDatabase := func(name string) *gorm.DB {
		db, err := gorm.Open(sqlite.Open(t.TempDir()+"/"+name+".db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
		return db
	}
	mainDB, logDB := newDatabase("main"), newDatabase("log")
	_, err := ResumeRoutingMigration(context.Background(), mainDB, "main")
	require.ErrorContains(t, err, "no recorded migration")
	mainReport, err := ApplyRoutingMigration(context.Background(), mainDB, "main")
	require.NoError(t, err)
	assert.Equal(t, "complete", mainReport.Stage)
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	require.NoError(t, logDB.Create(&Log{Id: 1, Other: `{"group_ratio":2,"billing_multiplier":3}`}).Error)
	_, err = ApplyRoutingMigration(context.Background(), logDB, "log")
	require.Error(t, err)
	require.Error(t, requireRoutingSchema(logDB, "log"))
	require.NoError(t, requireRoutingSchema(mainDB, "main"))
	require.NoError(t, logDB.Model(&Log{}).Where("id = 1").Update("other", `{"group_ratio":2}`).Error)
	logReport, err := ApplyRoutingMigration(context.Background(), logDB, "log")
	require.NoError(t, err)
	assert.Equal(t, "complete", logReport.Stage)
	_, err = VerifyRoutingMigration(context.Background(), mainDB, "main")
	require.NoError(t, err)
}
