package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type exportFailureWriter struct {
	*httptest.ResponseRecorder
	firstWrite func() error
	writes     int
}

func (w *exportFailureWriter) SetWriteDeadline(time.Time) error { return nil }

func (w *exportFailureWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		if err := w.firstWrite(); err != nil {
			return 0, err
		}
	}
	return w.ResponseRecorder.Write(p)
}

func TestLogExportFailureReleasesLock(t *testing.T) {
	for _, mode := range []string{"cancel", "write error"} {
		t.Run(mode, func(t *testing.T) {
			db, _ := newAuditTestDatabase(t, "sqlite", "")
			useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			const userID = 93001
			require.NoError(t, db.Create(&[]model.Log{
				{UserId: userID, CreatedAt: time.Now().Unix(), RequestId: "first"},
				{UserId: userID, CreatedAt: time.Now().Unix(), RequestId: "second"},
			}).Error)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &exportFailureWriter{ResponseRecorder: httptest.NewRecorder()}
			writer.firstWrite = func() error {
				// Failure is injected after the export has acquired its lock and
				// opened the query, rather than before entering the handler.
				release, acquired, err := service.AcquireLogExportLock(userID)
				if acquired {
					release()
				}
				require.NoError(t, err)
				require.False(t, acquired, "the active export must hold its lock")
				if mode == "cancel" {
					cancel()
					return nil
				}
				return errors.New("export writer failed")
			}
			engine := gin.New()
			engine.GET("/api/log/self/export", func(c *gin.Context) {
				c.Set("id", userID)
				c.Set("role", common.RoleAdminUser)
				ExportUserLogs(c)
			})
			request := httptest.NewRequest(http.MethodGet, "/api/log/self/export", nil).WithContext(ctx)
			engine.ServeHTTP(writer, request)
			require.Empty(t, logExportSlots, "failure or cancellation releases global slot")
			require.Positive(t, writer.writes, "the failure injection must execute")

			lines := strings.Split(strings.TrimSpace(writer.Body.String()), "\n")
			for _, line := range lines {
				var value map[string]any
				require.NoError(t, common.UnmarshalJsonStr(line, &value))
				if mode == "cancel" {
					require.NotContains(t, value, "success", "a canceled export must not claim completion")
				} else {
					require.Equal(t, false, value["success"])
					require.Equal(t, "Failed to export logs", value["message_key"])
				}
			}
			if mode == "cancel" {
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			} else {
				require.Len(t, lines, 1, "failed data writes must stop the stream")
			}

			release, acquired, err := service.AcquireLogExportLock(userID)
			if acquired {
				defer release()
			}
			require.NoError(t, err)
			require.True(t, acquired, "failure must release the export lock for a retry")
		})
	}
}

type exportShortWriter struct{}

func (exportShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestLogExportRejectsShortWrite(t *testing.T) {
	var output bytes.Buffer
	budget := logExportWriter{writer: &output, response: http.NewResponseController(&exportDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}), idleTimeout: time.Second}
	log := &model.Log{RequestId: "complete"}
	require.NoError(t, budget.write(log))
	require.Equal(t, 1, budget.count)
	budget.writer = exportShortWriter{}
	before := budget.count
	require.ErrorIs(t, budget.write(&model.Log{RequestId: "partial"}), io.ErrShortWrite)
	require.Equal(t, before, budget.count, "a partial row must not count as a successful export")
}

func TestLogExportRejectsUnsupportedDeadlineBeforeStreaming(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&model.Log{UserId: 1, Content: "must not stream"}).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/export", nil)
	c.Set("id", 1)
	c.Set("role", common.RoleAdminUser)
	ExportUserLogs(c)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	var result map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, false, result["success"])
	require.Equal(t, "Failed to export logs", result["message_key"])
	require.NotContains(t, w.Body.String(), "must not stream")
	require.Empty(t, logExportSlots)
}

type exportTrackedFailureWriter struct {
	*exportDeadlineWriter
	fail bool
}

func (w *exportTrackedFailureWriter) Write(p []byte) (int, error) {
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}

func TestLogExportCloseSkipsFlushAfterCancellationOrWriteFailure(t *testing.T) {
	for _, mode := range []string{"cancel", "write failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := &exportTrackedFailureWriter{exportDeadlineWriter: &exportDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/export", nil).WithContext(ctx)
			writer := newLogExportWriter(c)
			require.NoError(t, writer.write(&model.Log{}))
			if mode == "cancel" {
				cancel()
			} else {
				w.fail = true
				require.ErrorIs(t, writer.write(&model.Log{}), io.ErrClosedPipe)
			}
			writer.close()
			require.Zero(t, w.flushes)
			require.True(t, w.deadlines[len(w.deadlines)-1].IsZero())
		})
	}
}
