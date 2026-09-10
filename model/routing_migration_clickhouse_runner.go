package model

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func inspectClickHouseRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	r := RoutingMigrationReport{Version: RoutingSchemaVersion, Role: role, Dialect: "clickhouse", Stage: "unstarted", ToolVersion: RoutingMigrationToolVersion, RetiredColumns: map[string][]string{}, RetiredIndexes: map[string][]string{}, RetiredOptions: []string{}, Blockers: []string{}, Accounting: map[string]string{}}
	if role != "log" {
		return r, errors.New("ClickHouse routing migration is supported only for the independent log database")
	}
	identity, err := clickHouseRoutingIdentity(ctx, db)
	if err != nil {
		return r, err
	}
	r.DatabaseIdentity = identity
	state, err := loadRoutingMigrationState(db, role)
	if err != nil {
		return r, err
	}
	table := "logs"
	if state != nil {
		r.Stage, r.Cursor, r.SourceFingerprint = state.Stage, state.Cursor, state.SourceFingerprint
		if err := validateClickHouseMigrationState(state, identity); err != nil {
			r.Blockers = append(r.Blockers, err.Error())
			return r, nil
		}
		_, original := clickHouseMigrationTables(state)
		metadata, err := clickHouseRoutingMetadataFor(ctx, db, table)
		if err != nil {
			return r, err
		}
		if metadata == nil && state.MigrationID != "" {
			table = original
		}
	}
	metadata, err := clickHouseRoutingMetadataFor(ctx, db, table)
	if err != nil {
		return r, err
	}
	if metadata == nil {
		return r, nil
	}
	if err := validateClickHouseRoutingMetadata(metadata); err != nil {
		r.Blockers = append(r.Blockers, err.Error())
		return r, nil
	}
	r.Fingerprint, err = clickHouseMetadataFingerprint(*metadata)
	if err != nil {
		return r, err
	}
	for _, column := range metadata.Columns {
		if column.Name == "group" {
			r.RetiredColumns["logs"] = []string{"group"}
		}
	}
	partitions, err := clickHouseRoutingPartitionIDs(ctx, db, table)
	if err != nil {
		return r, err
	}
	snapshots := make([]clickHouseRoutingPartition, 0, len(partitions))
	for _, partition := range partitions {
		snapshot, err := scanClickHouseRoutingPartition(ctx, db, table, partition, metadata.Columns, false, nil)
		if err != nil {
			return r, fmt.Errorf("ClickHouse partition %s: %w", partition, err)
		}
		snapshots = append(snapshots, snapshot)
	}
	r.Accounting = clickHouseManifestAccounting(snapshots)
	return r, nil
}

func clickHouseManifestAccounting(partitions []clickHouseRoutingPartition) map[string]string {
	totals := map[string]*big.Int{"logs.rows": new(big.Int), "logs.quota": new(big.Int), "logs.prompt_tokens": new(big.Int), "logs.completion_tokens": new(big.Int)}
	for _, partition := range partitions {
		totals["logs.rows"].Add(totals["logs.rows"], big.NewInt(partition.Rows))
		for name, value := range partition.Accounting {
			n, _ := new(big.Int).SetString(value, 10)
			if n != nil {
				totals["logs."+name].Add(totals["logs."+name], n)
			}
		}
	}
	result := map[string]string{}
	for name, value := range totals {
		result[name] = value.String()
	}
	return result
}

func readClickHouseManifest(state *RoutingSchemaState) (clickHouseRoutingManifest, error) {
	var manifest clickHouseRoutingManifest
	if err := common.UnmarshalJsonStr(state.ClickHouseManifest, &manifest); err != nil {
		return manifest, errors.New("invalid ClickHouse migration manifest")
	}
	if err := validateClickHouseRoutingMetadata(&manifest.Source); err != nil {
		return manifest, err
	}
	fingerprint, err := clickHouseMetadataFingerprint(manifest.Source)
	if err != nil {
		return manifest, err
	}
	if fingerprint != state.SourceFingerprint {
		return manifest, errors.New("ClickHouse source metadata does not match its fingerprint")
	}
	previous := ""
	for _, partition := range manifest.Partitions {
		if partition.ID == "" || (previous != "" && partition.ID <= previous) || partition.Rows < 0 || len(partition.Digest) != 64 {
			return manifest, errors.New("invalid ClickHouse partition evidence")
		}
		if _, err := hex.DecodeString(partition.Digest); err != nil {
			return manifest, errors.New("invalid ClickHouse partition digest")
		}
		previous = partition.ID
		if len(partition.Accounting) != 3 {
			return manifest, errors.New("incomplete ClickHouse partition accounting")
		}
		for _, name := range []string{"quota", "prompt_tokens", "completion_tokens"} {
			if _, ok := new(big.Int).SetString(partition.Accounting[name], 10); !ok {
				return manifest, errors.New("invalid ClickHouse partition accounting")
			}
		}
	}
	encoded, err := common.Marshal(clickHouseManifestAccounting(manifest.Partitions))
	if err != nil {
		return manifest, err
	}
	if !equivalentRoutingJSON(encoded, []byte(state.BaselineAccounting)) {
		return manifest, errors.New("ClickHouse partition totals do not match baseline accounting")
	}
	if state.Cursor > int64(len(manifest.Partitions)) {
		return manifest, errors.New("ClickHouse migration cursor exceeds partition manifest")
	}
	return manifest, nil
}

func verifyClickHouseRoutingTable(ctx context.Context, db *gorm.DB, table string, manifest clickHouseRoutingManifest, fingerprint string, verifyJSON bool) error {
	metadata, err := clickHouseRoutingMetadataFor(ctx, db, table)
	if err != nil {
		return err
	}
	if metadata == nil {
		return fmt.Errorf("missing ClickHouse migration table %s", table)
	}
	actual, err := clickHouseMetadataFingerprint(*metadata)
	if err != nil {
		return err
	}
	if actual != fingerprint {
		return fmt.Errorf("ClickHouse metadata changed for %s", table)
	}
	partitions, err := clickHouseRoutingPartitionIDs(ctx, db, table)
	if err != nil {
		return err
	}
	if len(partitions) != len(manifest.Partitions) {
		return errors.New("ClickHouse partition set differs from source")
	}
	for i, expected := range manifest.Partitions {
		if partitions[i] != expected.ID {
			return errors.New("ClickHouse partition identity differs from source")
		}
		actual, err := scanClickHouseRoutingPartition(ctx, db, table, expected.ID, metadata.Columns, verifyJSON, nil)
		if err != nil {
			return err
		}
		if !sameClickHousePartition(expected, actual) {
			return fmt.Errorf("ClickHouse partition %s count, content or accounting mismatch", expected.ID)
		}
	}
	return nil
}

func beginClickHouseRoutingMigration(ctx context.Context, db *gorm.DB, report RoutingMigrationReport) (*RoutingSchemaState, error) {
	state, err := loadRoutingMigrationState(db, "log")
	if err != nil {
		return nil, err
	}
	if state != nil {
		return state, validateClickHouseMigrationState(state, report.DatabaseIdentity)
	}
	var leftovers uint64
	if err := db.WithContext(ctx).Raw("SELECT count() FROM system.tables WHERE database = currentDatabase() AND startsWith(name, 'routing_migration_')").Scan(&leftovers).Error; err != nil {
		return nil, err
	}
	if leftovers != 0 {
		return nil, errors.New("unowned ClickHouse migration table exists")
	}
	metadata, err := clickHouseRoutingMetadataFor(ctx, db, "logs")
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		if err := db.WithContext(ctx).Exec(clickHouseLogCreateTableSQL(0)).Error; err != nil {
			return nil, err
		}
		metadata, err = clickHouseRoutingMetadataFor(ctx, db, "logs")
		if err != nil {
			return nil, err
		}
	}
	fingerprint, err := clickHouseMetadataFingerprint(*metadata)
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	state = &RoutingSchemaState{Role: "log", Version: RoutingSchemaVersion, ToolVersion: RoutingMigrationToolVersion, DatabaseIdentity: report.DatabaseIdentity, MigrationID: hex.EncodeToString(nonce[:]), SourceFingerprint: fingerprint}
	if err := createClickHouseRoutingJournal(db.WithContext(ctx)); err != nil {
		return nil, err
	}
	return state, clickHouseMigrationCheckpoint(ctx, db, state, "ch_prepare", 0)
}

func prepareClickHouseRoutingCopy(ctx context.Context, db *gorm.DB, state *RoutingSchemaState) error {
	shadow, original := clickHouseMigrationTables(state)
	if metadata, err := clickHouseRoutingMetadataFor(ctx, db, original); err != nil {
		return err
	} else if metadata != nil {
		return errors.New("unexpected ClickHouse original table before copy")
	}
	if err := stopClickHouseRoutingMerges(ctx, db, "logs"); err != nil {
		return err
	}
	metadata, err := clickHouseRoutingMetadataFor(ctx, db, "logs")
	if err != nil {
		return err
	}
	if metadata == nil {
		return errors.New("missing ClickHouse source logs")
	}
	fingerprint, err := clickHouseMetadataFingerprint(*metadata)
	if err != nil {
		return err
	}
	if fingerprint != state.SourceFingerprint {
		return errors.New("ClickHouse source schema changed before copy")
	}
	manifest := clickHouseRoutingManifest{Source: *metadata, Partitions: []clickHouseRoutingPartition{}}
	if state.ClickHouseManifest != "" {
		manifest, err = readClickHouseManifest(state)
		if err != nil {
			return err
		}
		if err := verifyClickHouseRoutingTable(ctx, db, "logs", manifest, state.SourceFingerprint, false); err != nil {
			return err
		}
	} else {
		ids, err := clickHouseRoutingPartitionIDs(ctx, db, "logs")
		if err != nil {
			return err
		}
		for _, id := range ids {
			snapshot, err := scanClickHouseRoutingPartition(ctx, db, "logs", id, metadata.Columns, false, nil)
			if err != nil {
				return err
			}
			manifest.Partitions = append(manifest.Partitions, snapshot)
		}
		encoded, err := common.Marshal(manifest)
		if err != nil {
			return err
		}
		state.ClickHouseManifest = string(encoded)
		accounting, err := common.Marshal(clickHouseManifestAccounting(manifest.Partitions))
		if err != nil {
			return err
		}
		state.BaselineAccounting = string(accounting)
		if err := clickHouseMigrationCheckpoint(ctx, db, state, "ch_prepare", 0); err != nil {
			return err
		}
	}
	if existing, err := clickHouseRoutingMetadataFor(ctx, db, shadow); err != nil {
		return err
	} else if existing != nil {
		if err := db.WithContext(ctx).Exec("DROP TABLE ? SYNC", clause.Table{Name: shadow}).Error; err != nil {
			return err
		}
	}
	if err := db.WithContext(ctx).Exec("CREATE TABLE ? AS ?", clause.Table{Name: shadow}, clause.Table{Name: "logs"}).Error; err != nil {
		return err
	}
	if err := stopClickHouseRoutingMerges(ctx, db, shadow); err != nil {
		return err
	}
	for _, column := range metadata.Columns {
		if column.Name == "group" {
			if err := db.WithContext(ctx).Exec("ALTER TABLE ? DROP COLUMN ?", clause.Table{Name: shadow}, clause.Column{Name: "group"}).Error; err != nil {
				return fmt.Errorf("ClickHouse cannot retire group while preserving source table properties: %w", err)
			}
		}
	}
	target, err := clickHouseRoutingMetadataFor(ctx, db, shadow)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.New("ClickHouse shadow table was not created")
	}
	if target.EngineFull != metadata.EngineFull || target.PartitionKey != metadata.PartitionKey || target.SortingKey != metadata.SortingKey || target.PrimaryKey != metadata.PrimaryKey || target.SamplingKey != metadata.SamplingKey {
		return errors.New("ClickHouse shadow changed engine or keys")
	}
	state.TargetFingerprint, err = clickHouseMetadataFingerprint(*target)
	if err != nil {
		return err
	}
	return clickHouseMigrationCheckpoint(ctx, db, state, "ch_copy", 0)
}

func insertClickHouseRoutingBatch(ctx context.Context, db *gorm.DB, table string, batch []map[string]any) error {
	if len(batch) == 0 {
		return nil
	}
	names := make([]string, 0, len(batch[0]))
	for name := range batch[0] {
		names = append(names, name)
	}
	sort.Strings(names)
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = db.Statement.Quote(name)
	}
	placeholders := make([]string, 0, len(batch))
	values := []any{clause.Table{Name: table}}
	for _, row := range batch {
		markers := make([]string, len(names))
		for i, name := range names {
			markers[i] = "?"
			values = append(values, row[name])
		}
		placeholders = append(placeholders, "("+strings.Join(markers, ",")+")")
	}
	query := "INSERT INTO ? (" + strings.Join(quoted, ",") + ") SETTINGS insert_deduplicate = 0, async_insert = 0 VALUES " + strings.Join(placeholders, ",")
	return db.WithContext(ctx).Exec(query, values...).Error
}

func copyClickHouseRoutingPartitions(ctx context.Context, db *gorm.DB, state *RoutingSchemaState, manifest clickHouseRoutingManifest) error {
	shadow, _ := clickHouseMigrationTables(state)
	if err := stopClickHouseRoutingMerges(ctx, db, "logs"); err != nil {
		return err
	}
	if err := stopClickHouseRoutingMerges(ctx, db, shadow); err != nil {
		return err
	}
	if err := verifyClickHouseRoutingTable(ctx, db, "logs", manifest, state.SourceFingerprint, false); err != nil {
		return err
	}
	target, err := clickHouseRoutingMetadataFor(ctx, db, shadow)
	if err != nil {
		return err
	}
	if target == nil {
		return errors.New("missing ClickHouse shadow table")
	}
	fingerprint, err := clickHouseMetadataFingerprint(*target)
	if err != nil {
		return err
	}
	if fingerprint != state.TargetFingerprint {
		return errors.New("ClickHouse shadow schema differs from checkpoint")
	}
	for i, partition := range manifest.Partitions {
		if int64(i) < state.Cursor {
			actual, err := scanClickHouseRoutingPartition(ctx, db, shadow, partition.ID, target.Columns, true, nil)
			if err != nil {
				return err
			}
			if !sameClickHousePartition(actual, partition) {
				return errors.New("previously completed ClickHouse partition changed")
			}
			continue
		}
		// An acknowledgement may be lost after any inserted batch. Only the
		// owned incomplete partition is discarded; committed partitions remain.
		if err := db.WithContext(ctx).Exec("ALTER TABLE ? DROP PARTITION ID ?", clause.Table{Name: shadow}, partition.ID).Error; err != nil {
			return err
		}
		actual, err := scanClickHouseRoutingPartition(ctx, db, "logs", partition.ID, manifest.Source.Columns, false, func(batch []map[string]any) error { return insertClickHouseRoutingBatch(ctx, db, shadow, batch) })
		if err != nil {
			return err
		}
		if !sameClickHousePartition(actual, partition) {
			return errors.New("ClickHouse source partition changed during copy")
		}
		copied, err := scanClickHouseRoutingPartition(ctx, db, shadow, partition.ID, target.Columns, true, nil)
		if err != nil {
			return err
		}
		if !sameClickHousePartition(copied, partition) {
			return errors.New("ClickHouse copied partition differs from source")
		}
		if err := clickHouseMigrationCheckpoint(ctx, db, state, "ch_copy", int64(i+1)); err != nil {
			return err
		}
	}
	return clickHouseMigrationCheckpoint(ctx, db, state, "ch_verify", int64(len(manifest.Partitions)))
}

func activateClickHouseRoutingCopy(ctx context.Context, db *gorm.DB, state *RoutingSchemaState, manifest clickHouseRoutingManifest) error {
	shadow, original := clickHouseMigrationTables(state)
	if state.Stage == "ch_verify" {
		if err := stopClickHouseRoutingMerges(ctx, db, shadow); err != nil {
			return err
		}
		if err := verifyClickHouseRoutingTable(ctx, db, shadow, manifest, state.TargetFingerprint, true); err != nil {
			return err
		}
		old, err := clickHouseRoutingMetadataFor(ctx, db, original)
		if err != nil {
			return err
		}
		active, err := clickHouseRoutingMetadataFor(ctx, db, "logs")
		if err != nil {
			return err
		}
		if old == nil {
			if err := stopClickHouseRoutingMerges(ctx, db, "logs"); err != nil {
				return err
			}
			if err := verifyClickHouseRoutingTable(ctx, db, "logs", manifest, state.SourceFingerprint, false); err != nil {
				return err
			}
			if err := db.WithContext(ctx).Exec("RENAME TABLE ? TO ?", clause.Table{Name: "logs"}, clause.Table{Name: original}).Error; err != nil {
				return err
			}
		} else if active != nil {
			return errors.New("ambiguous ClickHouse source rename state")
		}
		if err := clickHouseMigrationCheckpoint(ctx, db, state, "ch_source_renamed", state.Cursor); err != nil {
			return err
		}
	}
	if state.Stage == "ch_source_renamed" {
		if err := stopClickHouseRoutingMerges(ctx, db, original); err != nil {
			return err
		}
		if err := verifyClickHouseRoutingTable(ctx, db, original, manifest, state.SourceFingerprint, false); err != nil {
			return err
		}
		active, err := clickHouseRoutingMetadataFor(ctx, db, "logs")
		if err != nil {
			return err
		}
		if active == nil {
			if err := verifyClickHouseRoutingTable(ctx, db, shadow, manifest, state.TargetFingerprint, true); err != nil {
				return err
			}
			if err := db.WithContext(ctx).Exec("RENAME TABLE ? TO ?", clause.Table{Name: shadow}, clause.Table{Name: "logs"}).Error; err != nil {
				return err
			}
		} else if remaining, err := clickHouseRoutingMetadataFor(ctx, db, shadow); err != nil {
			return err
		} else if remaining != nil {
			return errors.New("ambiguous ClickHouse target activation state")
		}
		if err := clickHouseMigrationCheckpoint(ctx, db, state, "ch_activated", state.Cursor); err != nil {
			return err
		}
	}
	if state.Stage == "ch_activated" {
		if err := stopClickHouseRoutingMerges(ctx, db, "logs"); err != nil {
			return err
		}
		if err := verifyClickHouseRoutingTable(ctx, db, "logs", manifest, state.TargetFingerprint, true); err != nil {
			return err
		}
		old, err := clickHouseRoutingMetadataFor(ctx, db, original)
		if err != nil {
			return err
		}
		if old != nil {
			if err := verifyClickHouseRoutingTable(ctx, db, original, manifest, state.SourceFingerprint, false); err != nil {
				return err
			}
			if err := db.WithContext(ctx).Exec("DROP TABLE ? SYNC", clause.Table{Name: original}).Error; err != nil {
				return err
			}
		}
		if err := clickHouseMigrationCheckpoint(ctx, db, state, "complete", state.Cursor); err != nil {
			return err
		}
	}
	return nil
}

func applyClickHouseRoutingMigration(ctx context.Context, db *gorm.DB, role string) (report RoutingMigrationReport, resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		state, err := loadRoutingMigrationState(db.WithContext(cleanup), role)
		if err == nil && state != nil && state.Stage != "complete" {
			report.Stage, report.Cursor = state.Stage, state.Cursor
			resultErr = fmt.Errorf("%w; ClickHouse migration remains at %s cursor %d; keep writers stopped and resume with this tool", resultErr, state.Stage, state.Cursor)
		}
	}()
	report, err := inspectClickHouseRoutingMigration(ctx, db, role)
	if err != nil {
		return report, err
	}
	if len(report.Blockers) > 0 {
		return report, errors.New("ClickHouse migration preflight has blockers")
	}
	if report.Stage == "complete" {
		return finishClickHouseRoutingMigration(ctx, db, role)
	}
	state, err := beginClickHouseRoutingMigration(ctx, db, report)
	if err != nil {
		return report, err
	}
	if state.Stage == "ch_prepare" {
		if err := prepareClickHouseRoutingCopy(ctx, db, state); err != nil {
			return report, err
		}
	}
	manifest, err := readClickHouseManifest(state)
	if err != nil {
		return report, err
	}
	if state.Stage == "ch_copy" {
		if err := copyClickHouseRoutingPartitions(ctx, db, state, manifest); err != nil {
			return report, err
		}
	}
	if err := activateClickHouseRoutingCopy(ctx, db, state, manifest); err != nil {
		return report, err
	}
	return finishClickHouseRoutingMigration(ctx, db, role)
}

func finishClickHouseRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	report, err := verifyClickHouseRoutingMigration(ctx, db, role)
	if err != nil {
		return report, err
	}
	if err := db.WithContext(ctx).Exec("SYSTEM START MERGES logs").Error; err != nil {
		return report, fmt.Errorf("ClickHouse migration verified but merges could not resume: %w", err)
	}
	return report, nil
}

func verifyClickHouseRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	report, err := inspectClickHouseRoutingMigration(ctx, db, role)
	if err != nil {
		return report, err
	}
	if len(report.Blockers) > 0 || len(report.RetiredColumns) > 0 {
		return report, errors.New("ClickHouse retired schema or blockers remain")
	}
	state, err := loadRoutingMigrationState(db, role)
	if err != nil {
		return report, err
	}
	if state == nil || state.Stage != "complete" {
		return report, errors.New("ClickHouse migration is incomplete")
	}
	metadata, err := clickHouseRoutingMetadataFor(ctx, db, "logs")
	if err != nil {
		return report, err
	}
	if metadata == nil {
		return report, errors.New("missing ClickHouse logs table")
	}
	if state.MigrationID == "" {
		partitions, err := clickHouseRoutingPartitionIDs(ctx, db, "logs")
		if err != nil {
			return report, err
		}
		for _, partition := range partitions {
			if _, err := scanClickHouseRoutingPartition(ctx, db, "logs", partition, metadata.Columns, true, nil); err != nil {
				return report, err
			}
		}
		return report, nil
	}
	manifest, err := readClickHouseManifest(state)
	if err != nil {
		return report, err
	}
	if err := verifyClickHouseRoutingTable(ctx, db, "logs", manifest, state.TargetFingerprint, true); err != nil {
		return report, err
	}
	shadow, original := clickHouseMigrationTables(state)
	for _, table := range []string{shadow, original} {
		if metadata, err := clickHouseRoutingMetadataFor(ctx, db, table); err != nil {
			return report, err
		} else if metadata != nil {
			return report, errors.New("ClickHouse migration copy tables remain")
		}
	}
	return report, nil
}
