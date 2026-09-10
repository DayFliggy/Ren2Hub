package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RoutingMigrationReport struct {
	Version           int                 `json:"version"`
	Role              string              `json:"role"`
	Dialect           string              `json:"dialect"`
	Fingerprint       string              `json:"fingerprint"`
	RetiredColumns    map[string][]string `json:"retired_columns"`
	Blockers          []string            `json:"blockers"`
	Accounting        map[string]string   `json:"accounting"`
	Stage             string              `json:"stage"`
	Cursor            int64               `json:"cursor"`
	ToolVersion       string              `json:"tool_version"`
	DatabaseIdentity  string              `json:"database_identity"`
	SourceFingerprint string              `json:"source_fingerprint"`
	RetiredIndexes    map[string][]string `json:"retired_indexes"`
	RetiredOptions    []string            `json:"retired_options"`
	CandidateDigest   string              `json:"candidate_digest"`
	CandidateCount    int64               `json:"candidate_count"`
	OrphanAbilities   int64               `json:"orphan_abilities"`
}

var routingRetiredColumns = map[string][]string{
	"tokens": {"group", "auto_groups", "cross_group_retry"}, "channels": {"group"},
	"users": {"group"}, "abilities": {"group"}, "channel_model_capabilities": {"ability_groups"},
	"tasks": {"group"}, "logs": {"group"}, "quota_data": {"use_group"}, "perf_metrics": {"group"}, "models": {"enable_groups"},
	"subscription_plans": {"upgrade_group", "downgrade_group"},
	"user_subscriptions": {"upgrade_group", "downgrade_group", "prev_user_group"},
}

func migrationColumnSet(db *gorm.DB, table string) (map[string]bool, error) {
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(columns))
	for _, column := range columns {
		set[column.Name()] = true
	}
	return set, nil
}

func InspectRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	if db.Dialector.Name() == "clickhouse" {
		return inspectClickHouseRoutingMigration(ctx, db, role)
	}
	r := RoutingMigrationReport{Version: RoutingSchemaVersion, Role: role, Dialect: db.Dialector.Name(), RetiredColumns: map[string][]string{}, RetiredIndexes: map[string][]string{}, RetiredOptions: []string{}, Blockers: []string{}, Accounting: map[string]string{}, Stage: "unstarted", ToolVersion: RoutingMigrationToolVersion}
	if role != "main" && role != "log" {
		return r, errors.New("invalid migration role")
	}
	identity, err := routingMigrationIdentity(ctx, db, role)
	if err != nil {
		return r, err
	}
	r.DatabaseIdentity = identity
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return r, err
	}
	sort.Strings(tables)
	var schema []string
	for _, table := range tables {
		if _, known := routingRetiredColumns[table]; !known {
			continue
		}
		if role == "log" && table != "logs" {
			continue
		}
		columns, err := migrationColumnSet(db, table)
		if err != nil {
			return r, err
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(routingMigrationModel(table)); err != nil {
			return r, err
		}
		knownColumns := make(map[string]bool, len(statement.Schema.FieldsByDBName))
		for name := range statement.Schema.FieldsByDBName {
			knownColumns[name] = true
		}
		for _, name := range routingRetiredColumns[table] {
			knownColumns[name] = true
		}
		for name := range columns {
			if !knownColumns[name] {
				r.Blockers = append(r.Blockers, "unsupported baseline column: "+table+"."+name)
			}
		}
		columnTypes, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return r, err
		}
		for _, column := range columnTypes {
			kind, _ := column.ColumnType()
			nullable, _ := column.Nullable()
			primary, _ := column.PrimaryKey()
			schema = append(schema, fmt.Sprintf("%s.%s:%s:nullable=%t:primary=%t", table, column.Name(), kind, nullable, primary))
		}
		for _, column := range routingRetiredColumns[table] {
			if columns[column] {
				r.RetiredColumns[table] = append(r.RetiredColumns[table], column)
			}
		}
		if r.Dialect != "clickhouse" {
			indexes, err := routingMigrationIndexes(db, table)
			if err != nil {
				return r, err
			}
			for _, index := range indexes {
				if index.Primary {
					continue
				}
				for _, column := range index.Columns {
					for _, retired := range routingRetiredColumns[table] {
						if column == retired {
							r.RetiredIndexes[table] = append(r.RetiredIndexes[table], index.Name)
						}
					}
				}
			}
		}
	}
	sort.Strings(schema)
	digest := sha256.Sum256([]byte(strings.Join(schema, "\n")))
	r.Fingerprint = hex.EncodeToString(digest[:])
	state, err := loadRoutingMigrationState(db, role)
	if err != nil {
		return r, err
	}
	if state != nil {
		r.Stage, r.Cursor, r.SourceFingerprint = state.Stage, state.Cursor, state.SourceFingerprint
		if err := validateRoutingMigrationState(state, identity); err != nil {
			r.Blockers = append(r.Blockers, err.Error())
		}
		var retiredIndexes map[string][]string
		if state.RetiredIndexes != "" {
			if err := common.UnmarshalJsonStr(state.RetiredIndexes, &retiredIndexes); err != nil {
				return r, errors.New("invalid recorded retired index evidence")
			}
		}
		for table, names := range retiredIndexes {
			for _, name := range names {
				if db.Migrator().HasTable(table) && db.Migrator().HasIndex(table, name) {
					r.RetiredIndexes[table] = append(r.RetiredIndexes[table], name)
				}
			}
		}
	}
	if role == "main" && r.Dialect != "clickhouse" {
		table := "abilities"
		if state != nil && state.MigrationID != "" {
			_, original := routingAbilityCopyTables(state)
			if !db.Migrator().HasTable(table) && db.Migrator().HasTable(original) {
				table = original
			}
		}
		r.CandidateDigest, r.CandidateCount, r.OrphanAbilities, err = routingCandidateSnapshot(ctx, db, table)
		if err != nil {
			return r, err
		}
		if state != nil && state.MigrationID != "" {
			r.OrphanAbilities = state.OrphanAbilities
		}
		if db.Migrator().HasTable("options") {
			var options []Option
			if err := db.WithContext(ctx).Find(&options).Error; err != nil {
				return r, err
			}
			for _, option := range options {
				if retiredMigrationOption(option.Key) {
					r.RetiredOptions = append(r.RetiredOptions, option.Key)
				}
			}
			sort.Strings(r.RetiredOptions)
		}
	}
	if role == "main" && db.Migrator().HasTable("users") {
		for table, required := range map[string][]string{"users": {"id", "quota", "auth_version"}, "tokens": {"id", "remain_quota"}, "channels": {"id", "capacity_total", "channel_ratio"}, "user_route_profiles": {"id", "mode", "version"}} {
			if !db.Migrator().HasTable(table) {
				r.Blockers = append(r.Blockers, "unsupported baseline: missing "+table)
				continue
			}
			columns, err := migrationColumnSet(db, table)
			if err != nil {
				return r, err
			}
			for _, column := range required {
				if !columns[column] {
					r.Blockers = append(r.Blockers, "unsupported baseline: missing "+table+"."+column)
				}
			}
		}
		if err := inspectRetiredTemplates(ctx, db, &r); err != nil {
			return r, err
		}
	} else if role == "main" {
		for _, table := range tables {
			if _, known := routingRetiredColumns[table]; known {
				r.Blockers = append(r.Blockers, "unsupported baseline: application tables exist without users")
				break
			}
		}
	}
	if r.Dialect != "clickhouse" {
		if err := walkMigrationJSON(ctx, db, role, false, func(path string, err error) { r.Blockers = append(r.Blockers, path+": "+err.Error()) }); err != nil {
			return r, err
		}
	}
	accounting, err := routingAccountingSnapshot(ctx, db, role)
	if err != nil {
		return r, err
	}
	r.Accounting = accounting
	return r, nil
}

func inspectRetiredTemplates(ctx context.Context, db *gorm.DB, r *RoutingMigrationReport) error {
	if db.Migrator().HasTable("channels") {
		rows, err := db.WithContext(ctx).Table("channels").Select("id", "param_override").Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var raw *string
			if err := rows.Scan(&id, &raw); err != nil {
				return err
			}
			if raw != nil && strings.TrimSpace(*raw) != "" {
				var value any
				if err := common.UnmarshalJsonStr(*raw, &value); err != nil {
					r.Blockers = append(r.Blockers, fmt.Sprintf("channels[%d].param_override has invalid JSON", id))
					continue
				}
				for _, path := range common.RetiredRoutingRulePaths(value) {
					r.Blockers = append(r.Blockers, fmt.Sprintf("channels[%d].param_override%s requires explicit template mapping", id, path))
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("options") {
		var options []Option
		if err := db.WithContext(ctx).Where("? LIKE ?", clause.Column{Name: "key"}, "channel_affinity%").Find(&options).Error; err != nil {
			return err
		}
		for _, option := range options {
			converted, err := migrateAffinityRuleJSON([]byte(option.Value))
			if err != nil {
				r.Blockers = append(r.Blockers, "options["+option.Key+"] has invalid affinity JSON")
				continue
			}
			var value any
			if err := common.Unmarshal(converted, &value); err != nil {
				r.Blockers = append(r.Blockers, "options["+option.Key+"] has invalid affinity JSON")
				continue
			}
			for _, path := range common.RetiredRoutingRulePaths(value) {
				r.Blockers = append(r.Blockers, "options["+option.Key+"]"+path+" requires explicit affinity mapping")
			}
		}
	}
	return nil
}

// Financial summaries use arbitrary precision. They never normalize or charge
// balances while moving schema, and cannot overflow SUM on SQLite integers.
func routingAccountingSnapshot(ctx context.Context, db *gorm.DB, role string) (map[string]string, error) {
	result := map[string]string{}
	fields := map[string][]string{"users": {"quota", "used_quota", "request_count"}, "tokens": {"remain_quota", "used_quota"}, "channels": {"used_quota"}, "tasks": {"quota"}, "user_subscriptions": {"amount_used"}, "logs": {"quota", "prompt_tokens", "completion_tokens"}, "quota_data": {"count", "quota", "token_used"}, "perf_metrics": {"request_count", "success_count", "total_latency_ms", "ttft_sum_ms", "ttft_count", "output_tokens", "generation_ms"}}
	for table, columns := range fields {
		if role == "log" && table != "logs" {
			continue
		}
		if !db.Migrator().HasTable(table) {
			continue
		}
		if table != "quota_data" && table != "perf_metrics" {
			var count int64
			if err := db.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
				return nil, err
			}
			result[table+".rows"] = strconv.FormatInt(count, 10)
		}
		available, err := migrationColumnSet(db, table)
		if err != nil {
			return nil, err
		}
		for _, column := range columns {
			if !available[column] {
				continue
			}
			query := db.WithContext(ctx).Table(table).Select(column)
			stableRows := table != "quota_data" && table != "perf_metrics" && db.Dialector.Name() != "clickhouse"
			if stableRows {
				query = query.Select("id", column).Order("id ASC")
			}
			rows, err := query.Rows()
			if err != nil {
				return nil, err
			}
			total := new(big.Int)
			digest := sha256.New()
			for rows.Next() {
				var id, raw any
				var scanErr error
				if stableRows {
					scanErr = rows.Scan(&id, &raw)
				} else {
					scanErr = rows.Scan(&raw)
				}
				if scanErr != nil {
					rows.Close()
					return nil, scanErr
				}
				if raw == nil {
					if stableRows {
						fmt.Fprintf(digest, "%s:null\n", migrationText(id))
					}
					continue
				}
				text := fmt.Sprint(raw)
				if b, ok := raw.([]byte); ok {
					text = string(b)
				}
				n, ok := new(big.Int).SetString(text, 10)
				if !ok {
					rows.Close()
					return nil, fmt.Errorf("non-integer accounting value in %s.%s", table, column)
				}
				total.Add(total, n)
				if stableRows {
					fmt.Fprintf(digest, "%s:%s\n", migrationText(id), n.String())
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			result[table+"."+column] = total.String()
			if stableRows {
				result[table+"."+column+".digest"] = hex.EncodeToString(digest.Sum(nil))
			}
		}
	}
	return result, nil
}

func walkMigrationJSON(ctx context.Context, db *gorm.DB, role string, write bool, blocked func(string, error)) error {
	for _, table := range []string{"tasks", "logs"} {
		if role == "log" && table != "logs" {
			continue
		}
		if err := walkRoutingJSONTable(ctx, db, table, write, false, blocked, nil); err != nil {
			return err
		}
	}
	return nil
}

func migrateRoutingJSON(ctx context.Context, db *gorm.DB, state *RoutingSchemaState) error {
	return walkRoutingJSONTable(ctx, db, strings.TrimPrefix(state.Stage, "json_"), true, false, nil, state)
}

func walkRoutingJSONTable(ctx context.Context, db *gorm.DB, table string, write, verify bool, blocked func(string, error), state *RoutingSchemaState) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	if err := requireRoutingPrimaryKey(db, table, "id"); err != nil {
		return err
	}
	column := "other"
	if table == "tasks" {
		column = "private_data"
	}
	var unsupportedIDs int64
	if err := db.WithContext(ctx).Table(table).Where("id <= 0").Count(&unsupportedIDs).Error; err != nil {
		return err
	}
	if unsupportedIDs != 0 {
		return fmt.Errorf("unsupported nonpositive primary key in %s", table)
	}
	cursor := int64(0)
	if state != nil {
		cursor = state.Cursor
	}
	for {
		var records []map[string]any
		query := db.WithContext(ctx).Table(table).Where("id > ?", cursor).Order("id ASC").Limit(200)
		if table == "tasks" {
			query = query.Select("id", column, "status")
		} else {
			query = query.Select("id", column)
		}
		if err := query.Find(&records).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		changes := make(map[int64]string, len(records))
		for _, record := range records {
			id, err := strconv.ParseInt(migrationText(record["id"]), 10, 64)
			if err != nil {
				return err
			}
			cursor = id
			raw := []byte(migrationText(record[column]))
			var converted []byte
			if table == "tasks" {
				status := migrationText(record["status"])
				converted, err = migrateTaskBillingJSON(raw, status != TaskStatusSuccess && status != TaskStatusFailure)
			} else {
				converted, err = migrateLogBillingJSON(raw)
			}
			if err != nil {
				if blocked != nil {
					blocked(fmt.Sprintf("%s[%d].%s", table, id, column), err)
					continue
				}
				return fmt.Errorf("%s[%d].%s: %w", table, id, column, err)
			}
			if write && string(converted) != string(raw) {
				changes[id] = string(converted)
			}
			if verify && !equivalentRoutingJSON(raw, converted) {
				return fmt.Errorf("unconverted billing JSON in %s[%d].%s", table, id, column)
			}
		}
		if !write {
			continue
		}
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, record := range records {
				id, _ := strconv.ParseInt(migrationText(record["id"]), 10, 64)
				if converted, changed := changes[id]; changed {
					if err := tx.Table(table).Where("id = ?", id).Update(column, converted).Error; err != nil {
						return err
					}
				}
			}
			if state != nil {
				return checkpointRoutingMigration(tx, state, state.Stage, cursor)
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

func migrationText(value any) string {
	if value == nil {
		return ""
	}
	if b, ok := value.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(value)
}

func ApplyRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	if db.Dialector.Name() == "clickhouse" {
		return applyClickHouseRoutingMigration(ctx, db, role)
	}
	report, err := InspectRoutingMigration(ctx, db, role)
	if err != nil {
		return report, err
	}
	if len(report.Blockers) > 0 {
		return report, errors.New("migration preflight has blocking records")
	}
	if report.Stage == "complete" && len(report.RetiredColumns) == 0 {
		return VerifyRoutingMigration(ctx, db, role)
	}
	state, err := beginRoutingMigration(ctx, db, report)
	if err != nil {
		return report, err
	}
	if err := verifyRoutingMigrationEvidence(ctx, db, state); err != nil {
		return report, err
	}
	for state.Stage != "complete" {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		current := state.Stage
		switch current {
		case "account_classes":
			if role == "main" {
				err = migrateAccountClassData(ctx, db)
			}
		case "abilities":
			// Copying and DDL are deferred until all monetary data has passed verification.
			err = verifyRoutingMigrationEvidence(ctx, db, state)
		case "templates":
			if role == "main" {
				err = migrateRoutingAffinityOptions(ctx, db, false)
			}
		case "json_tasks", "json_logs":
			if role == "main" || current == "json_logs" {
				err = migrateRoutingJSON(ctx, db, state)
			}
		case "summaries":
			if role == "main" {
				err = migrateRoutingSummaries(ctx, db)
			}
		case "verify_data":
			err = verifyRoutingMigrationEvidence(ctx, db, state)
		case "retire":
			err = retireRoutingSchema(ctx, db, state)
		case "verify_schema":
			err = verifyRetiredRoutingData(ctx, db, state)
		default:
			err = errors.New("unknown migration stage")
		}
		if err != nil {
			report.Stage, report.Cursor = state.Stage, state.Cursor
			return report, fmt.Errorf("migration stage %s cursor %d: %w", state.Stage, state.Cursor, err)
		}
		for i, stage := range routingMigrationStages {
			if stage == current {
				err = checkpointRoutingMigration(db.WithContext(ctx), state, routingMigrationStages[i+1], 0)
				break
			}
		}
		if err != nil {
			return report, err
		}
	}
	return VerifyRoutingMigration(ctx, db, role)
}

func retireRoutingSchema(ctx context.Context, db *gorm.DB, state *RoutingSchemaState) error {
	db = db.WithContext(ctx)
	tables := routingMigrationTables(state.Role)
	for i, table := range tables {
		if int64(i) < state.Cursor {
			continue
		}
		if table == "abilities" {
			if err := rebuildRetiredAbilities(ctx, db, state); err != nil {
				return err
			}
		} else if db.Migrator().HasTable(table) {
			for _, column := range routingRetiredColumns[table] {
				if db.Migrator().HasColumn(table, column) {
					if err := dropRetiredRoutingColumn(db, table, column); err != nil {
						return fmt.Errorf("drop %s.%s: %w", table, column, err)
					}
				}
			}
		}
		if err := checkpointRoutingMigration(db, state, "retire", int64(i+1)); err != nil {
			return err
		}
	}
	var indexes map[string][]string
	if err := common.UnmarshalJsonStr(state.RetiredIndexes, &indexes); err != nil {
		return err
	}
	for table, names := range indexes {
		for _, name := range names {
			if db.Migrator().HasTable(table) && db.Migrator().HasIndex(table, name) {
				if err := db.Migrator().DropIndex(table, name); err != nil {
					return err
				}
			}
		}
	}
	if state.Role == "main" {
		if db.Migrator().HasTable("options") {
			var options []Option
			if err := db.Find(&options).Error; err != nil {
				return err
			}
			for _, option := range options {
				if retiredMigrationOption(option.Key) {
					if err := db.Where(map[string]any{"key": option.Key}).Delete(&Option{}).Error; err != nil {
						return err
					}
				}
			}
		}
		if db.Migrator().HasTable("user_route_profiles") {
			if err := db.Table("user_route_profiles").Where("mode = ?", "legacy").Updates(map[string]any{"mode": RouteModeAutoLab, "active_group_id": nil, "version": gorm.Expr("version + ?", 1)}).Error; err != nil {
				return err
			}
		}
		if db.Migrator().HasTable("channel_model_capabilities") {
			if err := db.Exec("DELETE FROM channel_model_capabilities").Error; err != nil {
				return err
			}
		}
		if db.Migrator().HasTable("channel_capability_snapshots") {
			if err := db.Exec("DELETE FROM channel_capability_snapshots").Error; err != nil {
				return err
			}
		}
		if db.Migrator().HasTable(&PerfMetric{}) && !db.Migrator().HasIndex(&PerfMetric{}, "idx_perf_model_bucket") {
			if err := db.Migrator().CreateIndex(&PerfMetric{}, "idx_perf_model_bucket"); err != nil {
				return err
			}
		}
	}
	return nil
}

func dropRetiredRoutingColumn(db *gorm.DB, table, column string) error {
	if db.Dialector.Name() != "sqlite" {
		return db.Table(table).Migrator().DropColumn(routingMigrationModel(table), column)
	}
	// This driver version silently leaves trailing columns in its regex-based
	// table recreation. SQLite's native ALTER validates the actual schema.
	var indexes []struct {
		Name   string
		Origin string
	}
	if err := db.Raw("SELECT name, origin FROM pragma_index_list(?)", table).Scan(&indexes).Error; err != nil {
		return err
	}
	for _, index := range indexes {
		var columns []struct{ Name string }
		if err := db.Raw("SELECT name FROM pragma_index_info(?)", index.Name).Scan(&columns).Error; err != nil {
			return err
		}
		for _, entry := range columns {
			if entry.Name == column {
				if index.Origin != "c" {
					return fmt.Errorf("retired column %s.%s remains in a table constraint", table, column)
				}
				if err := db.Exec("DROP INDEX ?", clause.Column{Name: index.Name}).Error; err != nil {
					return err
				}
				break
			}
		}
	}
	return db.Exec("ALTER TABLE ? DROP COLUMN ?", clause.Table{Name: table}, clause.Column{Name: column}).Error
}

func VerifyRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	if db.Dialector.Name() == "clickhouse" {
		return verifyClickHouseRoutingMigration(ctx, db, role)
	}
	report, err := InspectRoutingMigration(ctx, db, role)
	if err != nil {
		return report, err
	}
	if len(report.RetiredColumns) > 0 || len(report.RetiredIndexes) > 0 || len(report.RetiredOptions) > 0 || len(report.Blockers) > 0 {
		return report, errors.New("retired schema or invalid records remain")
	}
	state, err := loadRoutingMigrationState(db, role)
	if err != nil {
		return report, err
	}
	if state == nil {
		return report, errors.New("migration completion evidence is missing")
	}
	if err := verifyRetiredRoutingData(ctx, db, state); err != nil {
		return report, err
	}
	return report, requireRoutingSchema(db, role)
}

func ResumeRoutingMigration(ctx context.Context, db *gorm.DB, role string) (RoutingMigrationReport, error) {
	state, err := loadRoutingMigrationState(db, role)
	if err != nil {
		return RoutingMigrationReport{}, err
	}
	if state == nil {
		return RoutingMigrationReport{}, errors.New("no recorded migration to resume; inspect and apply first")
	}
	return ApplyRoutingMigration(ctx, db, role)
}

func verifyRetiredRoutingData(ctx context.Context, db *gorm.DB, state *RoutingSchemaState) error {
	report, err := InspectRoutingMigration(ctx, db, state.Role)
	if err != nil {
		return err
	}
	if len(report.RetiredColumns) > 0 || len(report.RetiredIndexes) > 0 || len(report.RetiredOptions) > 0 || len(report.Blockers) > 0 {
		return errors.New("physical retirement verification failed")
	}
	for _, table := range []string{"tasks", "logs"} {
		if state.Role == "log" && table != "logs" {
			continue
		}
		if err := walkRoutingJSONTable(ctx, db, table, false, true, nil, nil); err != nil {
			return err
		}
	}
	if state.MigrationID == "" {
		return nil
	}
	if state.Role == "main" {
		if err := migrateRoutingAffinityOptions(ctx, db, true); err != nil {
			return err
		}
		if err := verifyRoutingTargetIndexes(db); err != nil {
			return err
		}
	}
	shadow, original := routingAbilityCopyTables(state)
	if db.Migrator().HasTable(shadow) || db.Migrator().HasTable(original) {
		return errors.New("unfinished ability copy tables remain")
	}
	return verifyRoutingMigrationEvidence(ctx, db, state)
}

func migrateRoutingAffinityOptions(ctx context.Context, db *gorm.DB, verify bool) error {
	if !db.Migrator().HasTable("options") {
		return nil
	}
	var options []Option
	if err := db.WithContext(ctx).Where("? LIKE ?", clause.Column{Name: "key"}, "channel_affinity%").Find(&options).Error; err != nil {
		return err
	}
	for _, option := range options {
		converted, err := migrateAffinityRuleJSON([]byte(option.Value))
		if err != nil {
			return fmt.Errorf("invalid affinity JSON in options[%s]", option.Key)
		}
		if equivalentRoutingJSON([]byte(option.Value), converted) {
			continue
		}
		if verify {
			return fmt.Errorf("unconverted affinity rule in options[%s]", option.Key)
		}
		if err := db.WithContext(ctx).Model(&Option{}).Where(map[string]any{"key": option.Key}).Update("value", string(converted)).Error; err != nil {
			return err
		}
	}
	return nil
}

func retiredMigrationOption(key string) bool {
	return retiredRoutingOption(key) || key == "GroupRatio" || key == "TopupGroupRatio" || strings.HasPrefix(key, "ModelRequestRateLimit")
}

func migrateAccountClassData(ctx context.Context, db *gorm.DB) error {
	if !db.Migrator().HasTable("users") {
		return nil
	}
	classes := map[string]float64{DefaultAccountClass: 1}
	var topup map[string]float64
	if db.Migrator().HasTable("options") {
		var options []Option
		if err := db.Where(map[string]any{"key": []string{"GroupRatio", "TopupGroupRatio"}}).Find(&options).Error; err != nil {
			return err
		}
		for _, option := range options {
			var values map[string]float64
			if common.UnmarshalJsonStr(option.Value, &values) != nil {
				return fmt.Errorf("invalid %s option", option.Key)
			}
			for code := range values {
				classes[code] = 1
			}
			if option.Key == "TopupGroupRatio" {
				topup = values
			}
		}
	}
	refs := []struct{ table, old, new string }{
		{"users", "group", "account_class"}, {"subscription_plans", "upgrade_group", "upgrade_account_class"}, {"subscription_plans", "downgrade_group", "downgrade_account_class"},
		{"user_subscriptions", "upgrade_group", "upgrade_account_class"}, {"user_subscriptions", "downgrade_group", "downgrade_account_class"}, {"user_subscriptions", "prev_user_group", "previous_account_class"},
	}
	for _, ref := range refs {
		if !db.Migrator().HasTable(ref.table) {
			continue
		}
		column := ref.old
		if !db.Migrator().HasColumn(ref.table, column) {
			column = ref.new
		}
		if !db.Migrator().HasColumn(ref.table, column) {
			continue
		}
		var codes []string
		quoted := db.Statement.Quote(column)
		if err := db.Table(ref.table).Distinct(quoted).Pluck(quoted, &codes).Error; err != nil {
			return err
		}
		for _, code := range codes {
			if code != "" {
				classes[code] = 1
			}
		}
	}
	for code := range classes {
		if value, ok := topup[code]; ok && value != 0 {
			classes[code] = value
		}
	}
	if err := db.AutoMigrate(&AccountClass{}); err != nil {
		return err
	}
	for code, multiplier := range classes {
		a := AccountClass{Code: code, Name: code, DefaultConcurrency: 5, TopupMultiplier: multiplier, PolicyVersion: 1}
		if err := a.Validate(); err != nil {
			return fmt.Errorf("account class migration: %w", err)
		}
		var existing AccountClass
		err := db.Where("code = ?", code).First(&existing).Error
		if err == nil {
			if existing.Code != code {
				return errors.New("account class code collation collision")
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := db.Create(&a).Error; err != nil {
			return err
		}
	}
	for _, ref := range refs {
		if !db.Migrator().HasTable(ref.table) || !db.Migrator().HasColumn(ref.table, ref.old) {
			continue
		}
		if db.Migrator().HasColumn(ref.table, ref.new) {
			return fmt.Errorf("both old and new classification columns exist in %s", ref.table)
		}
		if err := db.Table(ref.table).Migrator().RenameColumn(routingMigrationModel(ref.table), ref.old, ref.new); err != nil {
			return err
		}
	}
	for _, field := range []string{"ConcurrencyOverride", "RPMOverride", "PolicyVersion"} {
		if !db.Migrator().HasColumn(&User{}, field) {
			if err := db.Migrator().AddColumn(&User{}, field); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateRoutingSummaries(ctx context.Context, db *gorm.DB) error {
	if db.Migrator().HasTable("perf_metrics") && db.Migrator().HasColumn("perf_metrics", "group") {
		// Stable surviving row IDs make this transaction repeatable, including
		// when a previous transaction committed but its acknowledgement was lost.
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var merged []PerfMetric
			selectSQL := "MIN(id) AS id, model_name, bucket_ts, SUM(request_count) AS request_count, SUM(success_count) AS success_count, SUM(total_latency_ms) AS total_latency_ms, SUM(ttft_sum_ms) AS ttft_sum_ms, SUM(ttft_count) AS ttft_count, SUM(output_tokens) AS output_tokens, SUM(generation_ms) AS generation_ms"
			if err := tx.Table("perf_metrics").Select(selectSQL).Group("model_name, bucket_ts").Find(&merged).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM perf_metrics").Error; err != nil {
				return err
			}
			for _, row := range merged {
				values := map[string]any{"id": row.Id, "model_name": row.ModelName, "group": "", "bucket_ts": row.BucketTs, "request_count": row.RequestCount, "success_count": row.SuccessCount, "total_latency_ms": row.TotalLatencyMs, "ttft_sum_ms": row.TtftSumMs, "ttft_count": row.TtftCount, "output_tokens": row.OutputTokens, "generation_ms": row.GenerationMs}
				if err := tx.Table("perf_metrics").Create(values).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("quota_data") && db.Migrator().HasColumn("quota_data", "use_group") {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var rows []QuotaData
			if err := tx.Table("quota_data").Select("MIN(id) AS id, user_id, username, model_name, created_at, token_id, channel_id, node_name, SUM(count) AS count, SUM(quota) AS quota, SUM(token_used) AS token_used").Group("user_id, username, model_name, created_at, token_id, channel_id, node_name").Find(&rows).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM quota_data").Error; err != nil {
				return err
			}
			for _, row := range rows {
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	return nil
}

// OpenRoutingMigrationDatabase intentionally bypasses application AutoMigrate,
// background workers, authentication caches and account creation.
func OpenRoutingMigrationDatabase(role string) (*gorm.DB, error) {
	if role != "main" && role != "log" {
		return nil, errors.New("invalid migration role")
	}
	env := "SQL_DSN"
	if role == "log" {
		env = "LOG_SQL_DSN"
	}
	db, kind, err := chooseDB(env, role == "log")
	if err != nil {
		return nil, errors.New("cannot connect to migration database")
	}
	if role == "main" {
		common.SetMainDatabaseType(kind)
	} else {
		common.SetLogDatabaseType(kind)
	}
	initCol()
	db = db.Session(&gorm.Session{Logger: newGormLogger(io.Discard)})
	return db, nil
}

func RoutingMigrationDisplayError(err error) error { return sanitizeDBError(err) }

func routingMigrationModel(table string) any {
	switch table {
	case "users":
		return &User{}
	case "tokens":
		return &Token{}
	case "channels":
		return &Channel{}
	case "abilities":
		return &Ability{}
	case "channel_model_capabilities":
		return &ChannelModelCapability{}
	case "tasks":
		return &Task{}
	case "logs":
		return &Log{}
	case "quota_data":
		return &QuotaData{}
	case "perf_metrics":
		return &PerfMetric{}
	case "models":
		return &Model{}
	case "subscription_plans":
		return &SubscriptionPlan{}
	case "user_subscriptions":
		return &UserSubscription{}
	default:
		return nil
	}
}
