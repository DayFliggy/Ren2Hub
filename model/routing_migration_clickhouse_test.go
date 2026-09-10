package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func clickHouseRoutingTestMetadata() clickHouseRoutingMetadata {
	names := make([]string, 0, len(clickHouseRoutingTypes))
	for name := range clickHouseRoutingTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	metadata := clickHouseRoutingMetadata{Engine: "MergeTree", EngineFull: "MergeTree", PartitionKey: "toYYYYMM(toDateTime(created_at))", SortingKey: "created_at, request_id", PrimaryKey: "created_at, request_id", CreateTableQuery: "CREATE TABLE fixture.logs (`id` Int64) ENGINE = MergeTree ORDER BY id TTL toDateTime(created_at) + toIntervalDay(30) SETTINGS index_granularity = 8192"}
	for _, name := range names {
		metadata.Columns = append(metadata.Columns, clickHouseRoutingColumn{Name: name, Type: clickHouseRoutingTypes[name]})
	}
	return metadata
}

func TestClickHouseRoutingProjectionPreservesDuplicateRowsAndFrozenJSON(t *testing.T) {
	metadata := clickHouseRoutingTestMetadata()
	values := make([]any, len(metadata.Columns))
	for i, column := range metadata.Columns {
		if column.Type == "String" {
			values[i] = ""
		} else {
			values[i] = int64(0)
		}
		if column.Name == "group" {
			values[i] = "vip"
		}
		if column.Name == "quota" {
			values[i] = int32(7)
		}
		if column.Name == "other" {
			values[i] = `{"group_ratio":0,"unknown":{"value":9007199254740993}}`
		}
	}
	row, err := projectClickHouseRoutingRow(metadata.Columns, values, false)
	require.NoError(t, err)
	assert.NotContains(t, row, "group")
	assert.Contains(t, row["other"], `"billing_multiplier":0`)
	assert.Contains(t, row["other"], "9007199254740993")
	_, err = projectClickHouseRoutingRow(metadata.Columns, values, true)
	require.ErrorContains(t, err, "unconverted")
	a, b := newClickHouseRoutingAccumulator(), newClickHouseRoutingAccumulator()
	require.NoError(t, a.add(row))
	require.NoError(t, a.add(row))
	require.NoError(t, b.add(row))
	assert.Equal(t, int64(2), a.snapshot("203001").Rows)
	assert.Equal(t, "14", a.snapshot("203001").Accounting["quota"])
	assert.False(t, sameClickHousePartition(a.snapshot("203001"), b.snapshot("203001")), "duplicate multiplicity must be verified even though every id is zero")
	require.NoError(t, b.add(row))
	assert.True(t, sameClickHousePartition(a.snapshot("203001"), b.snapshot("203001")))
}

func TestClickHouseRoutingDigestIsOrderIndependentButChecksFullContent(t *testing.T) {
	left := map[string]any{"quota": int64(1), "prompt_tokens": int64(2), "completion_tokens": int64(3), "content": "a", "id": int64(0)}
	right := map[string]any{"quota": int64(1), "prompt_tokens": int64(2), "completion_tokens": int64(3), "content": "b", "id": int64(0)}
	a, b := newClickHouseRoutingAccumulator(), newClickHouseRoutingAccumulator()
	require.NoError(t, a.add(left))
	require.NoError(t, a.add(right))
	require.NoError(t, b.add(right))
	require.NoError(t, b.add(left))
	assert.True(t, sameClickHousePartition(a.snapshot("203001"), b.snapshot("203001")))
	c := newClickHouseRoutingAccumulator()
	require.NoError(t, c.add(left))
	require.NoError(t, c.add(left))
	assert.False(t, sameClickHousePartition(a.snapshot("203001"), c.snapshot("203001")), "equal counts and totals cannot hide changed row content")
}

func TestClickHouseRoutingMetadataRejectsUnknownEngineAndRetainsTTLInFingerprint(t *testing.T) {
	metadata := clickHouseRoutingTestMetadata()
	require.NoError(t, validateClickHouseRoutingMetadata(&metadata))
	source, err := clickHouseMetadataFingerprint(metadata)
	require.NoError(t, err)
	metadata.CreateTableQuery = "CREATE TABLE fixture.routing_migration_l_123 UUID 'changed' (`id` Int64) ENGINE = MergeTree ORDER BY id TTL toDateTime(created_at) + toIntervalDay(30) SETTINGS index_granularity = 8192"
	renamed, err := clickHouseMetadataFingerprint(metadata)
	require.NoError(t, err)
	assert.Equal(t, source, renamed, "table name and Atomic UUID are not business metadata")
	metadata.CreateTableQuery = "CREATE TABLE fixture.logs (`id` Int64) ENGINE = MergeTree ORDER BY id SETTINGS index_granularity = 8192"
	withoutTTL, err := clickHouseMetadataFingerprint(metadata)
	require.NoError(t, err)
	assert.NotEqual(t, source, withoutTTL)
	metadata.Engine = "ReplacingMergeTree"
	require.Error(t, validateClickHouseRoutingMetadata(&metadata))
	metadata.Engine = "MergeTree"
	metadata.Columns = append(metadata.Columns, clickHouseRoutingColumn{Name: "unknown_private", Type: "String"})
	require.Error(t, validateClickHouseRoutingMetadata(&metadata))
}

func TestClickHouseRoutingProjectionRejectsConflictingBillingJSON(t *testing.T) {
	columns := []clickHouseRoutingColumn{{Name: "other", Type: "String"}}
	for _, raw := range []string{`{"group_ratio":1,"billing_multiplier":2}`, `{"group_ratio":null}`, `{"billing_multiplier":-1}`, `invalid`} {
		_, err := projectClickHouseRoutingRow(columns, []any{raw}, false)
		assert.Error(t, err)
	}
}

// This test only opens an explicitly marked, empty container database. It is
// skipped in the normal Windows suite; the supervisor supplies the log DSN.
func TestRoutingAcceptanceClickHouse(t *testing.T) {
	if os.Getenv("REN2HUB_ISOLATED_ACCEPTANCE") != "synthetic-empty-database" || os.Getenv("REN2HUB_ACCEPTANCE_LOG_DSN") == "" {
		t.Skip("isolated ClickHouse acceptance runner only")
	}
	db, kind, err := chooseDB("REN2HUB_ACCEPTANCE_LOG_DSN", true)
	require.NoError(t, err)
	require.Equal(t, common.DatabaseTypeClickHouse, kind)
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Empty(t, tables, "ClickHouse acceptance refuses nonempty databases")
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Logf("ClickHouse version=%s", version)
	require.NoError(t, db.Exec(clickHouseLogCreateTableSQL(10000)).Error)
	require.NoError(t, db.Exec("ALTER TABLE logs ADD COLUMN `group` String DEFAULT ''").Error)
	raw := `{"group_ratio":0,"vendor":{"opaque":9007199254740993}}`
	for _, timestamp := range []int64{1893456000, 1893456000, 1893456000, 1924992000, 1924992000} {
		require.NoError(t, db.Exec("INSERT INTO logs (id, created_at, quota, prompt_tokens, completion_tokens, other, `group`) VALUES (0, ?, 7, 11, 3, ?, 'vip')", timestamp, raw).Error)
	}
	ctx := context.Background()
	before, err := InspectRoutingMigration(ctx, db, "log")
	require.NoError(t, err)
	assert.Equal(t, "5", before.Accounting["logs.rows"])
	interruptions := []string{"ch_copy", "ch_source_renamed", "ch_activated"}
	for _, stage := range interruptions {
		injected := errors.New("injected ClickHouse checkpoint failure")
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:interrupt_ch", func(tx *gorm.DB) {
			state, ok := tx.Statement.Dest.(*RoutingSchemaState)
			if ok && state.Stage == stage && (stage != "ch_copy" || state.Cursor == 1) {
				tx.AddError(injected)
			}
		}))
		_, err := ApplyRoutingMigration(ctx, db, "log")
		require.ErrorIs(t, err, injected)
		require.NoError(t, db.Callback().Create().Remove("test:interrupt_ch"))
		require.Error(t, requireRoutingSchema(db, "log"))
	}
	after, err := ResumeRoutingMigration(ctx, db, "log")
	require.NoError(t, err)
	assert.Equal(t, "complete", after.Stage)
	assert.Equal(t, before.Accounting, after.Accounting)
	assert.Empty(t, after.RetiredColumns)
	_, err = VerifyRoutingMigration(ctx, db, "log")
	require.NoError(t, err)
	var state RoutingSchemaState
	require.NoError(t, db.Order("updated_at DESC").First(&state, "role = ?", "log").Error)
	manifest, err := readClickHouseManifest(&state)
	require.NoError(t, err)
	require.Len(t, manifest.Partitions, 2)
	metadata, err := clickHouseRoutingMetadataFor(ctx, db, "logs")
	require.NoError(t, err)
	require.NotNil(t, metadata)
	assert.True(t, clickHouseCreateTableHasTTL(metadata.CreateTableQuery))
	assert.Equal(t, manifest.Source.EngineFull, metadata.EngineFull)
	assert.Equal(t, manifest.Source.PartitionKey, metadata.PartitionKey)
	assert.Equal(t, manifest.Source.SortingKey, metadata.SortingKey)
	assert.Equal(t, int64(3), manifest.Partitions[0].Rows)
	assert.Equal(t, int64(2), manifest.Partitions[1].Rows)
	var unconverted uint64
	require.NoError(t, db.Raw("SELECT count() FROM logs WHERE position(other, 'group_ratio') > 0").Scan(&unconverted).Error)
	assert.Zero(t, unconverted)
	var zeroIDs uint64
	require.NoError(t, db.Raw("SELECT count() FROM logs WHERE id = 0").Scan(&zeroIDs).Error)
	assert.Equal(t, uint64(5), zeroIDs)
	fmt.Printf("ROUTING_CLICKHOUSE_ACCEPTANCE_OK version=%s rows=5 partitions=2\n", version)
}
