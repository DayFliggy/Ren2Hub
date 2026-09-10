package model

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const RoutingMigrationToolVersion = "routing-retirement-v2"

var routingMigrationStages = []string{"account_classes", "abilities", "templates", "json_tasks", "json_logs", "summaries", "verify_data", "retire", "verify_schema", "complete"}

type routingMigrationIndex struct {
	Name    string
	Columns []string
	Primary bool
	Unique  bool
}

func routingMigrationIndexes(db *gorm.DB, table string) ([]routingMigrationIndex, error) {
	var result []routingMigrationIndex
	if db.Dialector.Name() == "sqlite" {
		var indexes []struct {
			Name, Origin string
			Unique       bool
		}
		if err := db.Raw("SELECT name, origin, \"unique\" FROM pragma_index_list(?)", table).Scan(&indexes).Error; err != nil {
			return nil, err
		}
		for _, index := range indexes {
			var columns []string
			if err := db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", index.Name).Scan(&columns).Error; err != nil {
				return nil, err
			}
			result = append(result, routingMigrationIndex{Name: index.Name, Columns: columns, Primary: index.Origin == "pk", Unique: index.Unique})
		}
		return result, nil
	}
	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		return nil, err
	}
	for _, index := range indexes {
		primary, _ := index.PrimaryKey()
		unique, _ := index.Unique()
		result = append(result, routingMigrationIndex{Name: index.Name(), Columns: index.Columns(), Primary: primary, Unique: unique})
	}
	return result, nil
}

func verifyRoutingTargetIndexes(db *gorm.DB) error {
	if db.Migrator().HasTable("abilities") {
		if err := requireRoutingPrimaryKey(db, "abilities", "channel_id,model"); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("perf_metrics") {
		indexes, err := routingMigrationIndexes(db, "perf_metrics")
		if err != nil {
			return err
		}
		for _, index := range indexes {
			if index.Name == "idx_perf_model_bucket" && index.Unique && strings.Join(index.Columns, ",") == "model_name,bucket_ts" {
				return nil
			}
		}
		return errors.New("performance model/bucket unique index is missing or invalid")
	}
	return nil
}

func requireRoutingPrimaryKey(db *gorm.DB, table, expected string) error {
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}
	var primary []string
	for _, column := range columns {
		if isPrimary, _ := column.PrimaryKey(); isPrimary {
			primary = append(primary, column.Name())
		}
	}
	sort.Strings(primary)
	if strings.Join(primary, ",") != expected {
		return fmt.Errorf("unsupported primary key in %s; expected %s", table, expected)
	}
	return nil
}

func routingMigrationIdentity(ctx context.Context, db *gorm.DB, role string) (string, error) {
	var identity string
	switch db.Dialector.Name() {
	case "sqlite":
		var databases []struct{ Name, File string }
		if err := db.WithContext(ctx).Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
			return "", err
		}
		for _, database := range databases {
			if database.Name != "main" {
				continue
			}
			if database.File == "" {
				return "", errors.New("offline migration requires a persistent SQLite database")
			}
			path, err := filepath.Abs(database.File)
			if err != nil {
				return "", err
			}
			identity = filepath.Clean(path)
		}
	case "mysql":
		var server struct {
			Database, Instance string
		}
		if err := db.WithContext(ctx).Raw("SELECT DATABASE() AS `database`, @@server_uuid AS instance").Scan(&server).Error; err != nil {
			return "", err
		}
		identity = server.Database + "\x00" + server.Instance
	case "postgres":
		var server struct {
			Database, Schema, Host string
			Port                   int
		}
		if err := db.WithContext(ctx).Raw("SELECT current_database() AS database, current_schema() AS schema, COALESCE(inet_server_addr()::text, 'local') AS host, COALESCE(inet_server_port(), 0) AS port").Scan(&server).Error; err != nil {
			return "", err
		}
		identity = fmt.Sprintf("%s\x00%s\x00%s\x00%d", server.Database, server.Schema, server.Host, server.Port)
	case "clickhouse":
		return "", nil
	default:
		return "", errors.New("unsupported migration database dialect")
	}
	if identity == "" {
		return "", errors.New("cannot identify migration database")
	}
	digest := sha256.Sum256([]byte(db.Dialector.Name() + "\x00" + role + "\x00" + identity))
	return hex.EncodeToString(digest[:]), nil
}

func loadRoutingMigrationState(db *gorm.DB, role string) (*RoutingSchemaState, error) {
	if !db.Migrator().HasTable(&RoutingSchemaState{}) {
		return nil, nil
	}
	var state RoutingSchemaState
	err := db.Where("role = ?", role).Order("updated_at DESC").First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &state, err
}

func validateRoutingMigrationState(state *RoutingSchemaState, identity string) error {
	if state.Version != RoutingSchemaVersion {
		return errors.New("migration schema version differs from this tool")
	}
	if state.Stage == "complete" && state.ToolVersion == "" {
		return nil // Fresh databases have no historical conversion evidence.
	}
	if state.ToolVersion != RoutingMigrationToolVersion {
		return errors.New("migration tool version differs; resume with the recorded tool")
	}
	if state.DatabaseIdentity != identity {
		return errors.New("migration database identity differs from its recorded source")
	}
	if state.SourceFingerprint == "" || len(state.MigrationID) != 32 || state.BaselineAccounting == "" {
		return errors.New("migration evidence is incomplete")
	}
	if _, err := hex.DecodeString(state.MigrationID); err != nil {
		return errors.New("invalid migration identifier")
	}
	for _, stage := range routingMigrationStages {
		if state.Stage == stage && state.Cursor >= 0 {
			return nil
		}
	}
	return errors.New("unknown migration stage or invalid cursor")
}

func beginRoutingMigration(ctx context.Context, db *gorm.DB, report RoutingMigrationReport) (*RoutingSchemaState, error) {
	state, err := loadRoutingMigrationState(db, report.Role)
	if err != nil {
		return nil, err
	}
	if state != nil {
		return state, validateRoutingMigrationState(state, report.DatabaseIdentity)
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		if strings.HasPrefix(table, "routing_migration_") {
			return nil, errors.New("unowned migration table exists; inspect its origin before continuing")
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	accounting, err := common.Marshal(report.Accounting)
	if err != nil {
		return nil, err
	}
	indexes, err := common.Marshal(report.RetiredIndexes)
	if err != nil {
		return nil, err
	}
	state = &RoutingSchemaState{Role: report.Role, Version: RoutingSchemaVersion, Stage: routingMigrationStages[0], SourceFingerprint: report.Fingerprint, DatabaseIdentity: report.DatabaseIdentity, ToolVersion: RoutingMigrationToolVersion, MigrationID: hex.EncodeToString(nonce[:]), BaselineAccounting: string(accounting), RetiredIndexes: string(indexes), CandidateDigest: report.CandidateDigest, CandidateCount: report.CandidateCount, OrphanAbilities: report.OrphanAbilities, UpdatedAt: time.Now().UnixNano()}
	if err := db.WithContext(ctx).AutoMigrate(&RoutingSchemaState{}); err != nil {
		return nil, err
	}
	return state, db.WithContext(ctx).Create(state).Error
}

func checkpointRoutingMigration(db *gorm.DB, state *RoutingSchemaState, stage string, cursor int64) error {
	state.Stage, state.Cursor, state.UpdatedAt = stage, cursor, time.Now().UnixNano()
	return db.Save(state).Error
}

func verifyRoutingMigrationEvidence(ctx context.Context, db *gorm.DB, state *RoutingSchemaState) error {
	var baseline map[string]string
	if err := common.UnmarshalJsonStr(state.BaselineAccounting, &baseline); err != nil || baseline == nil {
		return errors.New("invalid recorded accounting evidence")
	}
	current, err := routingAccountingSnapshot(ctx, db, state.Role)
	if err != nil {
		return err
	}
	if len(current) != len(baseline) {
		return errors.New("accounting columns differ from the migration source")
	}
	for key, expected := range baseline {
		if current[key] != expected {
			return fmt.Errorf("accounting mismatch: %s", key)
		}
	}
	if state.Role == "main" {
		table := "abilities"
		_, original := routingAbilityCopyTables(state)
		if !db.Migrator().HasTable(table) && db.Migrator().HasTable(original) {
			table = original
		}
		digest, count, _, err := routingCandidateSnapshot(ctx, db, table)
		if err != nil {
			return err
		}
		if digest != state.CandidateDigest || count != state.CandidateCount {
			return errors.New("candidate set differs from the migration source")
		}
	}
	return nil
}

func routingMigrationTables(role string) []string {
	tables := make([]string, 0, len(routingRetiredColumns))
	for table := range routingRetiredColumns {
		if role == "main" || table == "logs" {
			tables = append(tables, table)
		}
	}
	sort.Strings(tables)
	return tables
}
