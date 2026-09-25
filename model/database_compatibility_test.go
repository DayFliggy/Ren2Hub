package model

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSQLiteConnectionWritePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.db") + "?_pragma=foreign_keys(1)&_txlock=deferred"
	db, err := openSQLiteDatabase(path)
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	assert.Equal(t, 1, connection.Stats().MaxOpenConnections)
	var mode string
	require.NoError(t, db.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	assert.Equal(t, "wal", mode)
	var timeout, foreignKeys int
	require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&timeout).Error)
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error)
	assert.Equal(t, 30000, timeout)
	assert.Equal(t, 1, foreignKeys)

	// An otherwise empty transaction must immediately reserve the writer lock.
	other, err := openSQLiteDatabase(path)
	require.NoError(t, err)
	otherSQL, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, otherSQL.Close()) })
	require.NoError(t, other.Exec("PRAGMA busy_timeout = 0").Error)
	tx, err := connection.BeginTx(context.Background(), &sql.TxOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	blocked := other.Begin()
	require.Error(t, blocked.Error, "a second transaction cannot acquire the reserved writer lock")
	require.NoError(t, tx.Rollback())
	next := other.Begin()
	require.NoError(t, next.Error)
	require.NoError(t, next.Rollback().Error)
}

func TestJSONValueStrictDriverContract(t *testing.T) {
	for _, input := range []any{[]byte(`["模型",9007199254740993]`), `["模型",9007199254740993]`} {
		var value JSONValue
		require.NoError(t, value.Scan(input))
		driverValue, err := value.Value()
		require.NoError(t, err)
		assert.IsType(t, "", driverValue)
		assert.JSONEq(t, `["模型",9007199254740993]`, driverValue.(string))
	}
	buffer := []byte(`{"ok":true}`)
	var copied JSONValue
	require.NoError(t, copied.Scan(buffer))
	buffer[0] = '['
	assert.Equal(t, `{"ok":true}`, string(copied))
	for _, invalid := range []any{42, map[string]any{"ok": true}, "", []byte("not json")} {
		require.Error(t, copied.Scan(invalid))
		assert.Equal(t, `{"ok":true}`, string(copied), "failed scans leave the prior value untouched")
	}
	require.NoError(t, copied.Scan(nil))
	assert.Nil(t, copied)
	value, err := copied.Value()
	require.NoError(t, err)
	assert.Nil(t, value)
	_, err = JSONValue("broken").Value()
	require.Error(t, err)
}

func TestDatabaseIntegrationConfiguration(t *testing.T) {
	dialects := os.Getenv("REQUIRE_DATABASE_DIALECTS")
	if dialects == "" {
		dialects = os.Getenv("REQUIRE_DATABASE_DIALECT")
	}
	for _, dialect := range strings.Split(dialects, ",") {
		dialect = strings.TrimSpace(dialect)
		if dialect == "" {
			continue
		}
		require.Contains(t, []string{"mysql", "postgres"}, dialect)
		prefix := "MYSQL"
		if dialect == "postgres" {
			prefix = "POSTGRES"
		}
		require.NotEmpty(t, strings.TrimSpace(os.Getenv("TEST_"+prefix+"_DSN")), "required integration DSN is missing")
		require.NotEmpty(t, strings.TrimSpace(os.Getenv("GATEWAY_STARTUP_"+prefix+"_DSN")), "required startup DSN is missing")
		require.NotEmpty(t, strings.TrimSpace(os.Getenv("BILLING_TEST_"+prefix+"_DSN")), "required billing DSN is missing")
		require.NotEmpty(t, strings.TrimSpace(os.Getenv("AUDIT_"+prefix+"_DSN")), "required audit DSN is missing")
	}
}

func TestPrefillGroupCompatibilityAcrossDatabases(t *testing.T) {
	for _, dialect := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL} {
		t.Run(string(dialect), func(t *testing.T) {
			var dsn string
			if dialect != common.DatabaseTypeSQLite {
				env := "TEST_MYSQL_DSN"
				if dialect == common.DatabaseTypePostgreSQL {
					env = "TEST_POSTGRES_DSN"
				}
				dsn = os.Getenv(env)
				if dsn == "" {
					t.Skip(env + " is not configured; compatibility remains unverified")
				}
			}
			db, err := gatewayStartupDatabase(t, dialect, dsn)()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, connection.Close()) })
			if dialect != common.DatabaseTypeSQLite {
				require.NoError(t, db.AutoMigrate(&PrefillGroup{}))
				// Imported databases may carry a global unique object under an
				// arbitrary name. The replacement must inspect its definition.
				require.NoError(t, db.Exec("CREATE UNIQUE INDEX ? ON prefill_groups (name)", clause.Column{Name: "imported_prefill_name"}).Error)
				if dialect == common.DatabaseTypePostgreSQL {
					require.NoError(t, db.Exec("ALTER TABLE prefill_groups ADD CONSTRAINT old_prefill_name UNIQUE (name)").Error)
				}
				require.NoError(t, db.Exec("CREATE UNIQUE INDEX keep_prefill_name_type ON prefill_groups (name, type)").Error)
			}
			require.NoError(t, migratePrefillGroups(db))
			if dialect != common.DatabaseTypeSQLite {
				assert.False(t, db.Migrator().HasIndex(&PrefillGroup{}, "imported_prefill_name"))
				assert.True(t, db.Migrator().HasIndex(&PrefillGroup{}, "keep_prefill_name_type"))
				// Composite constraints retain their own semantics; remove only
				// this test fixture before checking deleted-name reuse below.
				require.NoError(t, db.Migrator().DropIndex(&PrefillGroup{}, "keep_prefill_name_type"))
			}
			original := PrefillGroup{Name: "可复用组", Type: "model", Items: JSONValue(`["模型",9007199254740993]`)}
			require.NoError(t, db.Create(&original).Error)
			duplicate := PrefillGroup{Name: original.Name, Type: "model"}
			require.Error(t, db.Create(&duplicate).Error)
			var read PrefillGroup
			require.NoError(t, db.First(&read, original.Id).Error)
			assert.JSONEq(t, string(original.Items), string(read.Items))
			require.NoError(t, db.Delete(&original).Error)
			for range 2 {
				replacement := PrefillGroup{Name: original.Name, Type: "model", Items: JSONValue(`[]`)}
				require.NoError(t, db.Create(&replacement).Error)
				require.NoError(t, db.Delete(&replacement).Error)
			}
			recorder := &migrationSQLRecorder{}
			require.NoError(t, migratePrefillGroups(db.Session(&gorm.Session{Logger: recorder})))
			assert.Empty(t, recorder.schemaMutations(), "restart must not repeat DDL")
			active := PrefillGroup{Name: original.Name, Type: "model"}
			require.NoError(t, db.Create(&active).Error)
			require.NoError(t, migratePrefillGroups(db))
			active.Id = 0
			require.Error(t, db.Create(&active).Error, "restart must retain active-name uniqueness")
		})
	}
}

func TestPoolerErrorHintDoesNotExposeQueryData(t *testing.T) {
	for _, code := range []string{"08P01", "42P05"} {
		err := sanitizeDBError(&pgconn.PgError{Code: code, Message: "private payload"})
		assert.Contains(t, err.Error(), code)
		assert.Contains(t, err.Error(), "both pgx and GORM prepared statements must be disabled")
		assert.NotContains(t, err.Error(), "private payload")
	}
}
