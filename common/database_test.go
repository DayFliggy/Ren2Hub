package common

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDSNPreservesPathAndEnforcesWritePolicy(t *testing.T) {
	for _, path := range []string{
		"one-api.db",
		`C:\custom directory\main.db?cache=shared&_busy_timeout=1&_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_txlock=deferred`,
		"file:custom.db?mode=rwc&_pragma=busy_timeout%3D1",
		":memory:",
	} {
		t.Run(path, func(t *testing.T) {
			dsn, err := SQLiteDSN(path)
			require.NoError(t, err)
			filename, raw, _ := strings.Cut(dsn, "?")
			originalFilename, _, _ := strings.Cut(path, "?")
			assert.Equal(t, originalFilename, filename)
			query, err := url.ParseQuery(raw)
			require.NoError(t, err)
			assert.Equal(t, "immediate", query.Get("_txlock"))
			assert.Contains(t, query["_pragma"], "busy_timeout(30000)")
			assert.Contains(t, query["_pragma"], "journal_mode(WAL)")
			assert.NotContains(t, query["_pragma"], "journal_mode(DELETE)")
			if strings.Contains(path, "foreign_keys") {
				assert.Contains(t, query["_pragma"], "foreign_keys(1)")
				assert.Equal(t, "shared", query.Get("cache"))
			}
			again, err := SQLiteDSN(dsn)
			require.NoError(t, err)
			assert.Equal(t, dsn, again)
		})
	}
	for _, path := range []string{"", "main.db?bad=%zz", "main.db?_pragma="} {
		_, err := SQLiteDSN(path)
		require.Error(t, err)
	}
}
