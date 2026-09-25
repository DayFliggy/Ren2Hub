package common

import (
	"fmt"
	"net/url"
	"strings"
)

type DatabaseType string

const (
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypeSQLite     DatabaseType = "sqlite"
	DatabaseTypePostgreSQL DatabaseType = "postgres"
	DatabaseTypeClickHouse DatabaseType = "clickhouse"
)

var mainDatabaseType = DatabaseTypeSQLite
var logDatabaseType = DatabaseTypeSQLite

func MainDatabaseType() DatabaseType {
	return mainDatabaseType
}

func LogDatabaseType() DatabaseType {
	return logDatabaseType
}

func SetMainDatabaseType(databaseType DatabaseType) {
	mainDatabaseType = databaseType
}

func SetLogDatabaseType(databaseType DatabaseType) {
	logDatabaseType = databaseType
}

func SetDatabaseTypes(mainType DatabaseType, logType DatabaseType) {
	mainDatabaseType = mainType
	logDatabaseType = logType
}

func UsingMainDatabase(databaseType DatabaseType) bool {
	return mainDatabaseType == databaseType
}

func UsingLogDatabase(databaseType DatabaseType) bool {
	return logDatabaseType == databaseType
}

var SQLitePath = "one-api.db"

// SQLiteDSN applies the same locking policy to default and custom database paths.
// Keep unrelated driver options, but never allow a supplied DSN to weaken the
// single-writer transaction or busy-timeout policy.
func SQLiteDSN(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("SQLite database path is empty")
	}
	filename, query, _ := strings.Cut(path, "?")
	options, err := url.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("invalid SQLite connection options: %w", err)
	}
	pragmas := make([]string, 0, len(options["_pragma"])+2)
	for _, pragma := range options["_pragma"] {
		name := strings.ToLower(strings.TrimSpace(pragma))
		parts := strings.FieldsFunc(name, func(r rune) bool { return r == '(' || r == '=' })
		if len(parts) == 0 {
			return "", fmt.Errorf("empty SQLite pragma")
		}
		name = strings.TrimSpace(parts[0])
		if name != "busy_timeout" && name != "journal_mode" {
			pragmas = append(pragmas, pragma)
		}
	}
	options.Del("_busy_timeout")
	options.Del("_journal_mode")
	options.Set("_txlock", "immediate")
	options["_pragma"] = append(pragmas, "busy_timeout(30000)", "journal_mode(WAL)")
	return filename + "?" + options.Encode(), nil
}
