package model

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const prefillGroupNameIndex = "uk_prefill_name"

// MySQL does not implement partial indexes. NULL in a generated column gives
// deleted rows the same name-reuse semantics as PostgreSQL and SQLite.
const prefillGroupActiveNameIndex = "uk_prefill_active_name"

func migratePrefillGroups(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate prefill groups: database is nil")
	}
	if db.Migrator().HasTable(&PrefillGroup{}) {
		if db.Dialector.Name() == "mysql" {
			if err := migrateMySQLPrefillGroupUniqueness(db); err != nil {
				return err
			}
		} else if err := migratePrefillGroupUniqueness(db); err != nil {
			return err
		}
	}
	if err := db.AutoMigrate(&PrefillGroup{}); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return migrateMySQLPrefillGroupUniqueness(db)
	}
	if err := createPrefillGroupNameIndex(db); err != nil {
		return err
	}
	if db.Dialector.Name() == "postgres" {
		state, err := inspectPrefillGroupNameIndex(db, db.NamingStrategy.TableName("PrefillGroup"))
		if err != nil {
			return err
		}
		if !state.valid {
			return fmt.Errorf("prefill group index %q has an unexpected definition", prefillGroupNameIndex)
		}
	}
	return nil
}

func createPrefillGroupNameIndex(db *gorm.DB) error {
	if db.Migrator().HasIndex(&PrefillGroup{}, prefillGroupNameIndex) {
		return nil
	}
	return db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ? ON ? (?) WHERE deleted_at IS NULL",
		clause.Column{Name: prefillGroupNameIndex},
		clause.Table{Name: db.NamingStrategy.TableName("PrefillGroup")}, clause.Column{Name: "name"}).Error
}

func migrateMySQLPrefillGroupUniqueness(db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasColumn(&PrefillGroup{}, "DeletedAt") {
		if err := migrator.AddColumn(&PrefillGroup{}, "DeletedAt"); err != nil {
			return err
		}
	}
	table := db.NamingStrategy.TableName("PrefillGroup")
	var columns []struct {
		DataType   string
		Length     int64
		Expression string
	}
	if err := db.Raw(`SELECT DATA_TYPE AS data_type, CHARACTER_MAXIMUM_LENGTH AS length,
GENERATION_EXPRESSION AS expression FROM information_schema.columns
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'active_name'`, table).Scan(&columns).Error; err != nil {
		return err
	}
	if len(columns) == 0 {
		if err := db.Exec("ALTER TABLE ? ADD COLUMN active_name VARCHAR(64) GENERATED ALWAYS AS (IF(deleted_at IS NULL, name, NULL)) VIRTUAL",
			clause.Table{Name: table}).Error; err != nil {
			return fmt.Errorf("add prefill group active name: %w", err)
		}
	} else {
		expression := strings.NewReplacer("`", "", " ", "", "(", "", ")", "", "\n", "", "\t", "").Replace(strings.ToLower(columns[0].Expression))
		if columns[0].DataType != "varchar" || columns[0].Length != 64 || expression != "ifdeleted_atisnull,name,null" {
			return fmt.Errorf("prefill group active_name has an unexpected generated definition")
		}
	}
	var indexes []struct {
		Name      string
		Columns   string `gorm:"column:column_names"`
		NonUnique int
		Prefixed  int
	}
	if err := db.Raw(`SELECT INDEX_NAME AS name, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) AS column_names,
MAX(NON_UNIQUE) AS non_unique, MAX(SUB_PART IS NOT NULL) AS prefixed
FROM information_schema.statistics WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
GROUP BY INDEX_NAME`, table).Scan(&indexes).Error; err != nil {
		return err
	}
	found := false
	for _, index := range indexes {
		if index.Name == prefillGroupActiveNameIndex {
			if index.Columns != "active_name" || index.NonUnique != 0 || index.Prefixed != 0 {
				return fmt.Errorf("prefill group index %q has an unexpected definition", prefillGroupActiveNameIndex)
			}
			found = true
		}
	}
	if !found {
		if err := db.Exec("CREATE UNIQUE INDEX ? ON ? (active_name)", clause.Column{Name: prefillGroupActiveNameIndex}, clause.Table{Name: table}).Error; err != nil {
			return err
		}
	}
	// Install the replacement before dropping global uniqueness. MySQL DDL
	// commits implicitly, so a failure must never leave active names unprotected.
	for _, index := range indexes {
		if index.Name != "PRIMARY" && index.NonUnique == 0 && index.Columns == "name" && index.Prefixed == 0 {
			if err := migrator.DropIndex(&PrefillGroup{}, index.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

type conflictingPrefillGroupUniqueness struct {
	constraints []string
	indexes     []string
}

type prefillGroupNameIndexState struct {
	exists bool
	valid  bool
}

func (conflicts conflictingPrefillGroupUniqueness) empty() bool {
	return len(conflicts.constraints) == 0 && len(conflicts.indexes) == 0
}

func inspectConflictingPrefillGroupUniqueness(db *gorm.DB, tableName string) (conflictingPrefillGroupUniqueness, error) {
	var conflicts conflictingPrefillGroupUniqueness
	if err := db.Raw(`
SELECT constraint_meta.conname
FROM pg_catalog.pg_constraint AS constraint_meta
WHERE constraint_meta.conrelid = to_regclass(?)
  AND constraint_meta.contype = 'u'
  AND cardinality(constraint_meta.conkey) = 1
  AND EXISTS (
      SELECT 1
      FROM pg_catalog.pg_attribute AS attribute_meta
      WHERE attribute_meta.attrelid = constraint_meta.conrelid
        AND attribute_meta.attnum = constraint_meta.conkey[1]
        AND attribute_meta.attname = ?
  )
ORDER BY constraint_meta.conname`, tableName, "name").Scan(&conflicts.constraints).Error; err != nil {
		return conflicts, fmt.Errorf("inspect conflicting prefill group unique constraints: %w", err)
	}

	if err := db.Raw(`
SELECT index_class.relname
FROM pg_catalog.pg_index AS index_meta
JOIN pg_catalog.pg_class AS index_class
  ON index_class.oid = index_meta.indexrelid
JOIN pg_catalog.pg_attribute AS attribute_meta
  ON attribute_meta.attrelid = index_meta.indrelid
 AND attribute_meta.attnum = index_meta.indkey[0]
WHERE index_meta.indrelid = to_regclass(?)
  AND index_meta.indisunique
  AND NOT index_meta.indisprimary
  AND index_meta.indpred IS NULL
  AND index_meta.indexprs IS NULL
  AND index_meta.indnatts = 1
  AND attribute_meta.attname = ?
  AND NOT EXISTS (
      SELECT 1
      FROM pg_catalog.pg_constraint AS constraint_meta
      WHERE constraint_meta.conindid = index_meta.indexrelid
  )
ORDER BY index_class.relname`, tableName, "name").Scan(&conflicts.indexes).Error; err != nil {
		return conflicts, fmt.Errorf("inspect conflicting prefill group unique indexes: %w", err)
	}

	return conflicts, nil
}

func inspectPrefillGroupNameIndex(db *gorm.DB, tableName string) (prefillGroupNameIndexState, error) {
	var state struct {
		Exists bool `gorm:"column:index_exists"`
		Valid  bool `gorm:"column:index_valid"`
	}
	if err := db.Raw(`
SELECT count(*) > 0 AS index_exists,
       COALESCE(bool_or(
           index_meta.indisunique
           AND index_meta.indisvalid
           AND index_meta.indisready
           AND NOT index_meta.indisprimary
           AND index_meta.indexprs IS NULL
           AND index_meta.indnatts = 1
           AND attribute_meta.attname = ?
           AND pg_get_expr(index_meta.indpred, index_meta.indrelid) = '(deleted_at IS NULL)'
       ), false) AS index_valid
FROM pg_catalog.pg_index AS index_meta
JOIN pg_catalog.pg_class AS index_class
  ON index_class.oid = index_meta.indexrelid
LEFT JOIN pg_catalog.pg_attribute AS attribute_meta
  ON attribute_meta.attrelid = index_meta.indrelid
 AND attribute_meta.attnum = index_meta.indkey[0]
WHERE index_meta.indrelid = to_regclass(?)
  AND index_class.relname = ?`, "name", tableName, prefillGroupNameIndex).Scan(&state).Error; err != nil {
		return prefillGroupNameIndexState{}, fmt.Errorf("inspect prefill group partial unique index: %w", err)
	}
	return prefillGroupNameIndexState{exists: state.Exists, valid: state.Valid}, nil
}

// migratePrefillGroupUniqueness replaces global PostgreSQL uniqueness on name
// before AutoMigrate inspects the column. Match the definition rather than the
// object name, which can change across older schemas and database imports.
// Composite, expression and partial indexes retain their separate semantics.
func migratePrefillGroupUniqueness(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate prefill group uniqueness: database is nil")
	}
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&PrefillGroup{}); err != nil {
		return fmt.Errorf("parse prefill group schema: %w", err)
	}
	tableName := statement.Schema.Table
	conflicts, err := inspectConflictingPrefillGroupUniqueness(db, tableName)
	if err != nil {
		return err
	}
	if conflicts.empty() {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		migrator := tx.Migrator()
		if !migrator.HasTable(&PrefillGroup{}) {
			return nil
		}

		if err := tx.Exec(
			"LOCK TABLE ? IN ACCESS EXCLUSIVE MODE",
			clause.Table{Name: tableName},
		).Error; err != nil {
			return fmt.Errorf("lock prefill groups for uniqueness migration: %w", err)
		}

		conflicts, err := inspectConflictingPrefillGroupUniqueness(tx, tableName)
		if err != nil {
			return err
		}
		if conflicts.empty() {
			return nil
		}
		if !migrator.HasColumn(&PrefillGroup{}, "DeletedAt") {
			if err := migrator.AddColumn(&PrefillGroup{}, "DeletedAt"); err != nil {
				return fmt.Errorf("add prefill groups deleted_at column: %w", err)
			}
		}

		// A global index or constraint may already use the target index name.
		// Drop it before creating the replacement under the exclusive table lock;
		// the transaction restores all old objects if any migration step fails.
		for _, constraintName := range conflicts.constraints {
			if err := migrator.DropConstraint(&PrefillGroup{}, constraintName); err != nil {
				return fmt.Errorf("drop conflicting prefill group constraint %q: %w", constraintName, err)
			}
		}
		for _, indexName := range conflicts.indexes {
			if err := migrator.DropIndex(&PrefillGroup{}, indexName); err != nil {
				return fmt.Errorf("drop conflicting prefill group index %q: %w", indexName, err)
			}
		}

		targetIndex, err := inspectPrefillGroupNameIndex(tx, tableName)
		if err != nil {
			return err
		}
		if !targetIndex.exists {
			if err := createPrefillGroupNameIndex(tx); err != nil {
				return fmt.Errorf("create prefill group partial unique index: %w", err)
			}
			targetIndex, err = inspectPrefillGroupNameIndex(tx, tableName)
			if err != nil {
				return err
			}
		}
		if !targetIndex.valid {
			return fmt.Errorf("prefill group index %q has an unexpected definition", prefillGroupNameIndex)
		}

		return nil
	})
}
