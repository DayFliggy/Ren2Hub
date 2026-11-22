package controller

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSelfLogStatIdentityDatabaseMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		kind      common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite}, {"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL}, {"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" && os.Getenv(tc.env) == "" {
				t.Skip(tc.env + " is not configured")
			}
			db, _ := newAuditTestDatabase(t, tc.name, os.Getenv(tc.env))
			useLogExportDatabases(t, db, db, tc.kind, tc.kind)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			now := time.Now().Unix()
			require.NoError(t, db.Create(&[]model.Log{
				{UserId: 1, Username: "previous-name", CreatedAt: now, Type: model.LogTypeConsume, Quota: 3, PromptTokens: 2, CompletionTokens: 3},
				{UserId: 1, Username: "probe%", CreatedAt: now, Type: model.LogTypeConsume, Quota: 4, PromptTokens: 1, CompletionTokens: 3},
				{UserId: 2, Username: "probe-other", CreatedAt: now, Type: model.LogTypeConsume, Quota: 99, PromptTokens: 99},
			}).Error)
			result := callLogHandler(t, GetLogsSelfStat, 1, common.RoleCommonUser, "username=probe-other&user_id=2", "probe%")[0]
			require.Equal(t, true, result["success"])
			data := result["data"].(map[string]any)
			require.EqualValues(t, 7, data["quota"])
			require.EqualValues(t, 2, data["rpm"])
			require.EqualValues(t, 9, data["tpm"])
			// Administrator searches retain their explicit wildcard contract.
			admin := callLogHandler(t, GetLogsStat, 3, common.RoleAdminUser, "username=probe%25")[0]
			require.EqualValues(t, 103, admin["data"].(map[string]any)["quota"])
		})
	}
}

func TestLogExportReportsLimitsAndReleasesLock(t *testing.T) {
	for _, mode := range []string{"rows", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			db, _ := newAuditTestDatabase(t, "sqlite", "")
			useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Channel{}))
			count, content := 10001, "small"
			if mode == "bytes" {
				count, content = 2, strings.Repeat("x", 17*1024*1024)
			}
			logs := make([]model.Log, count)
			for i := range logs {
				logs[i] = model.Log{UserId: 1, CreatedAt: time.Now().Unix(), Content: content, RequestId: fmt.Sprint(i)}
			}
			require.NoError(t, db.CreateInBatches(logs, 100).Error)
			lines := callLogHandler(t, ExportUserLogs, 1, common.RoleAdminUser, "")
			last := lines[len(lines)-1]
			require.Equal(t, false, last["success"])
			require.Equal(t, "LOG_EXPORT_LIMIT_EXCEEDED", last["code"])
			data := last["data"].(map[string]any)
			require.Equal(t, true, data["truncated"])
			require.EqualValues(t, len(lines)-1, data["count"])
			if mode == "rows" {
				require.Len(t, lines, 10001)
				frontendFixture(t, "log-export-limit.json", last)
			} else {
				require.Len(t, lines, 2)
			}
			// A bounded export must release the same per-user lock for a retry.
			retry := callLogHandler(t, ExportUserLogs, 1, common.RoleAdminUser, "request_id=0")
			require.Equal(t, true, retry[len(retry)-1]["success"])
		})
	}
}
