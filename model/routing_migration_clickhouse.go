package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type clickHouseRoutingColumn struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	DefaultKind       string `json:"default_kind"`
	DefaultExpression string `json:"default_expression"`
}

type clickHouseRoutingMetadata struct {
	Engine           string                    `json:"engine"`
	EngineFull       string                    `json:"engine_full"`
	PartitionKey     string                    `json:"partition_key"`
	SortingKey       string                    `json:"sorting_key"`
	PrimaryKey       string                    `json:"primary_key"`
	SamplingKey      string                    `json:"sampling_key"`
	CreateTableQuery string                    `json:"create_table_query"`
	Columns          []clickHouseRoutingColumn `json:"columns" gorm:"-"`
}

type clickHouseRoutingPartition struct {
	ID         string            `json:"id"`
	Rows       int64             `json:"rows"`
	Digest     string            `json:"digest"`
	Accounting map[string]string `json:"accounting"`
}

type clickHouseRoutingManifest struct {
	Source     clickHouseRoutingMetadata    `json:"source"`
	Partitions []clickHouseRoutingPartition `json:"partitions"`
}

var clickHouseRoutingTypes = map[string]string{
	"id": "Int64", "user_id": "Int32", "created_at": "Int64", "type": "Int32",
	"content": "String", "username": "String", "token_name": "String", "model_name": "String",
	"quota": "Int32", "prompt_tokens": "Int32", "completion_tokens": "Int32", "use_time": "Int32",
	"is_stream": "UInt8", "channel_id": "Int32", "token_id": "Int32", "ip": "String",
	"request_id": "String", "upstream_request_id": "String", "other": "String", "group": "String",
}

func createClickHouseRoutingJournal(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS routing_schema_states (role String, version Int32, stage String, source_fingerprint String, cursor Int64, updated_at Int64, tool_version String, database_identity String, migration_id String, baseline_accounting String, candidate_digest String, candidate_count Int64, orphan_abilities Int64, ability_step String, retired_indexes String, click_house_manifest String, target_fingerprint String) ENGINE = MergeTree ORDER BY (role, updated_at)`).Error
}

func clickHouseRoutingIdentity(ctx context.Context, db *gorm.DB) (string, error) {
	var server struct{ Database, Host, DatabaseUUID string }
	if err := db.WithContext(ctx).Raw("SELECT currentDatabase() AS database, hostName() AS host, toString(uuid) AS database_uuid FROM system.databases WHERE name = currentDatabase()").Scan(&server).Error; err != nil {
		return "", err
	}
	if server.Database == "" || server.Host == "" {
		return "", errors.New("cannot identify ClickHouse log database")
	}
	digest := sha256.Sum256([]byte("clickhouse\x00log\x00" + server.Host + "\x00" + server.Database + "\x00" + server.DatabaseUUID))
	return hex.EncodeToString(digest[:]), nil
}

func clickHouseRoutingMetadataFor(ctx context.Context, db *gorm.DB, table string) (*clickHouseRoutingMetadata, error) {
	var rows []clickHouseRoutingMetadata
	if err := db.WithContext(ctx).Raw("SELECT engine, engine_full, partition_key, sorting_key, primary_key, sampling_key, create_table_query FROM system.tables WHERE database = currentDatabase() AND name = ?", table).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, errors.New("ambiguous ClickHouse table metadata")
	}
	metadata := rows[0]
	if err := db.WithContext(ctx).Raw("SELECT name, type, default_kind, default_expression FROM system.columns WHERE database = currentDatabase() AND table = ? ORDER BY position", table).Scan(&metadata.Columns).Error; err != nil {
		return nil, err
	}
	return &metadata, nil
}

func validateClickHouseRoutingMetadata(metadata *clickHouseRoutingMetadata) error {
	if metadata.Engine != "MergeTree" {
		return errors.New("unsupported ClickHouse log engine; only the baseline MergeTree is accepted")
	}
	seen := map[string]bool{}
	for _, column := range metadata.Columns {
		kind, ok := clickHouseRoutingTypes[column.Name]
		if !ok || kind != column.Type || (column.DefaultKind != "" && column.DefaultKind != "DEFAULT") {
			return fmt.Errorf("unsupported ClickHouse log column %s", column.Name)
		}
		seen[column.Name] = true
	}
	for name := range clickHouseRoutingTypes {
		if name != "group" && !seen[name] {
			return fmt.Errorf("missing baseline ClickHouse log column %s", name)
		}
	}
	return nil
}

func clickHouseMetadataFingerprint(metadata clickHouseRoutingMetadata) (string, error) {
	// SHOW CREATE canonicalizes the body. Renaming a table changes only the
	// header (and Atomic UUID), so the body covers TTL, codecs and settings too.
	start := strings.Index(metadata.CreateTableQuery, "(")
	if start < 0 {
		return "", errors.New("invalid ClickHouse CREATE TABLE metadata")
	}
	metadata.CreateTableQuery = metadata.CreateTableQuery[start:]
	encoded, err := common.Marshal(metadata)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func clickHouseMigrationTables(state *RoutingSchemaState) (string, string) {
	return "routing_migration_l_" + state.MigrationID, "routing_migration_old_l_" + state.MigrationID
}

func clickHouseMigrationCheckpoint(ctx context.Context, db *gorm.DB, state *RoutingSchemaState, stage string, cursor int64) error {
	state.Stage, state.Cursor = stage, cursor
	now := time.Now().UnixNano()
	if now <= state.UpdatedAt {
		now = state.UpdatedAt + 1
	}
	state.UpdatedAt = now
	// The journal is append-only; UPDATE cannot provide a synchronous durable
	// checkpoint in ClickHouse. Reads always select the newest journal row.
	ctx = chdriver.Context(ctx, chdriver.WithSettings(chdriver.Settings{"async_insert": 0, "insert_deduplicate": 0}))
	return db.WithContext(ctx).Create(state).Error
}

func validateClickHouseMigrationState(state *RoutingSchemaState, identity string) error {
	if state.Version != RoutingSchemaVersion {
		return errors.New("ClickHouse routing schema version differs")
	}
	if state.Stage == "complete" && state.ToolVersion == "" {
		return nil
	}
	if state.ToolVersion != RoutingMigrationToolVersion || state.DatabaseIdentity != identity {
		return errors.New("ClickHouse migration tool or database identity differs")
	}
	if len(state.MigrationID) != 32 || state.SourceFingerprint == "" {
		return errors.New("incomplete ClickHouse migration evidence")
	}
	if _, err := hex.DecodeString(state.MigrationID); err != nil {
		return errors.New("invalid ClickHouse migration identity")
	}
	switch state.Stage {
	case "ch_prepare", "ch_copy", "ch_verify", "ch_source_renamed", "ch_activated", "complete":
		if state.Cursor >= 0 {
			return nil
		}
	}
	return errors.New("unknown ClickHouse migration checkpoint")
}

func stopClickHouseRoutingMerges(ctx context.Context, db *gorm.DB, table string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var mutations uint64
	if err := db.WithContext(ctx).Raw("SELECT count() FROM system.mutations WHERE database = currentDatabase() AND table = ? AND is_done = 0", table).Scan(&mutations).Error; err != nil {
		return err
	}
	if mutations != 0 {
		return errors.New("ClickHouse table has unfinished mutations")
	}
	if err := db.WithContext(ctx).Exec("SYSTEM STOP MERGES ?", clause.Table{Name: table}).Error; err != nil {
		return err
	}
	for {
		var active uint64
		if err := db.WithContext(ctx).Raw("SELECT count() FROM system.merges WHERE database = currentDatabase() AND table = ?", table).Scan(&active).Error; err != nil {
			return err
		}
		if active == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

func clickHouseRoutingPartitionIDs(ctx context.Context, db *gorm.DB, table string) ([]string, error) {
	var ids []string
	if err := db.WithContext(ctx).Table(table).Distinct("_partition_id").Order("_partition_id").Pluck("_partition_id", &ids).Error; err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

type clickHouseRoutingAccumulator struct {
	rows       int64
	digest     big.Int
	accounting map[string]*big.Int
}

func newClickHouseRoutingAccumulator() *clickHouseRoutingAccumulator {
	return &clickHouseRoutingAccumulator{accounting: map[string]*big.Int{"quota": new(big.Int), "prompt_tokens": new(big.Int), "completion_tokens": new(big.Int)}}
}

func (a *clickHouseRoutingAccumulator) add(row map[string]any) error {
	if a.rows == int64(^uint64(0)>>1) {
		return errors.New("ClickHouse row count overflow")
	}
	a.rows++
	for name, total := range a.accounting {
		value, ok := new(big.Int).SetString(migrationText(row[name]), 10)
		if !ok {
			return fmt.Errorf("invalid ClickHouse accounting column %s", name)
		}
		total.Add(total, value)
	}
	encoded, err := common.Marshal(row)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	a.digest.Add(&a.digest, new(big.Int).SetBytes(digest[:]))
	a.digest.Mod(&a.digest, new(big.Int).Lsh(big.NewInt(1), 256))
	return nil
}

func (a *clickHouseRoutingAccumulator) snapshot(partition string) clickHouseRoutingPartition {
	accounting := map[string]string{}
	for name, total := range a.accounting {
		accounting[name] = total.String()
	}
	return clickHouseRoutingPartition{ID: partition, Rows: a.rows, Digest: fmt.Sprintf("%064x", &a.digest), Accounting: accounting}
}

func projectClickHouseRoutingRow(columns []clickHouseRoutingColumn, values []any, verify bool) (map[string]any, error) {
	if len(columns) != len(values) {
		return nil, errors.New("ClickHouse row width differs from source metadata")
	}
	row := make(map[string]any, len(values))
	for i, column := range columns {
		if column.Name == "group" {
			continue
		}
		value := migrationText(values[i])
		if values[i] == nil {
			return nil, fmt.Errorf("unexpected NULL in ClickHouse column %s", column.Name)
		}
		if column.Type == "String" {
			if column.Name == "other" {
				converted, err := migrateLogBillingJSON([]byte(value))
				if err != nil {
					return nil, fmt.Errorf("invalid ClickHouse billing JSON: %w", err)
				}
				if verify && !equivalentRoutingJSON([]byte(value), converted) {
					return nil, errors.New("unconverted ClickHouse billing JSON")
				}
				value = string(converted)
			}
			row[column.Name] = value
		} else {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid ClickHouse integer in %s", column.Name)
			}
			row[column.Name] = n
		}
	}
	return row, nil
}

// Reads every physical row, including duplicate id=0 rows. The multiset digest
// is independent of merge/sort order and counts duplicate rows separately.
func scanClickHouseRoutingPartition(ctx context.Context, db *gorm.DB, table, partition string, columns []clickHouseRoutingColumn, verify bool, write func([]map[string]any) error) (clickHouseRoutingPartition, error) {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, db.Statement.Quote(column.Name))
	}
	rows, err := db.WithContext(ctx).Table(table).Select(strings.Join(names, ",")).Where("_partition_id = ?", partition).Rows()
	if err != nil {
		return clickHouseRoutingPartition{}, err
	}
	defer rows.Close()
	acc := newClickHouseRoutingAccumulator()
	batch := make([]map[string]any, 0, 200)
	batchBytes := 0
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return clickHouseRoutingPartition{}, err
		}
		row, err := projectClickHouseRoutingRow(columns, values, verify)
		if err != nil {
			return clickHouseRoutingPartition{}, err
		}
		if err := acc.add(row); err != nil {
			return clickHouseRoutingPartition{}, err
		}
		if write != nil {
			rowBytes := 0
			for _, value := range row {
				if text, ok := value.(string); ok {
					rowBytes += len(text)
				} else {
					rowBytes += 8
				}
			}
			if len(batch) > 0 && rowBytes > 8*1024*1024-batchBytes {
				if err := write(batch); err != nil {
					return clickHouseRoutingPartition{}, err
				}
				batch = make([]map[string]any, 0, 200)
				batchBytes = 0
			}
			batch = append(batch, row)
			batchBytes += rowBytes
			if len(batch) == 200 || batchBytes >= 8*1024*1024 {
				if err := write(batch); err != nil {
					return clickHouseRoutingPartition{}, err
				}
				batch = make([]map[string]any, 0, 200)
				batchBytes = 0
			}
		}
	}
	if err := rows.Err(); err != nil {
		return clickHouseRoutingPartition{}, err
	}
	if write != nil && len(batch) > 0 {
		if err := write(batch); err != nil {
			return clickHouseRoutingPartition{}, err
		}
	}
	return acc.snapshot(partition), nil
}

func sameClickHousePartition(left, right clickHouseRoutingPartition) bool {
	if left.ID != right.ID || left.Rows != right.Rows || left.Digest != right.Digest || len(left.Accounting) != len(right.Accounting) {
		return false
	}
	for name, value := range left.Accounting {
		if right.Accounting[name] != value {
			return false
		}
	}
	return true
}
