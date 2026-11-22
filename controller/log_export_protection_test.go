package controller

import (
	"bufio"
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogExportBusy(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&[]model.Log{{UserId: 1, CreatedAt: time.Now().Unix()}, {UserId: 2, CreatedAt: time.Now().Unix()}}).Error)
	entered, resume, done := make(chan struct{}, 2), make(chan struct{}), make(chan struct{}, 2)
	for _, id := range []int{1, 2} {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			w := &exportFailureWriter{ResponseRecorder: httptest.NewRecorder(), firstWrite: func() error { entered <- struct{}{}; <-resume; return nil }}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/export", nil)
			c.Set("id", id)
			c.Set("role", common.RoleAdminUser)
			ExportUserLogs(c)
		}(id)
	}
	<-entered
	<-entered
	result := callLogHandler(t, ExportUserLogs, 3, common.RoleAdminUser, "")[0]
	close(resume)
	<-done
	<-done
	require.Equal(t, "LOG_EXPORT_BUSY", result["code"])
	require.Equal(t, "Log export is busy. Try again later.", result["message_key"])
	frontendFixture(t, "log-export-busy.json", result)
	require.Equal(t, true, callLogHandler(t, ExportUserLogs, 3, common.RoleAdminUser, "")[0]["success"])
}

func TestLogExportKeysetDatabaseMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		kind      common.DatabaseType
	}{{"sqlite", "", common.DatabaseTypeSQLite}, {"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL}, {"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" && os.Getenv(tc.env) == "" {
				t.Skip(tc.env)
			}
			db, _ := newAuditTestDatabase(t, tc.name, os.Getenv(tc.env))
			useLogExportDatabases(t, db, db, tc.kind, tc.kind)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			logs := make([]model.Log, 12000)
			now := time.Now().Unix()
			for i := range logs {
				logs[i] = model.Log{UserId: 1, CreatedAt: now - int64(i/700), RequestId: fmt.Sprint(i)}
			}
			require.NoError(t, db.CreateInBatches(logs, 100).Error)
			for _, all := range []bool{true, false} {
				handler := ExportUserLogs
				if all {
					handler = ExportAllLogs
				}
				lines := callLogHandler(t, handler, 1, common.RoleAdminUser, "")
				require.Len(t, lines, 12001)
				require.EqualValues(t, 12000, lines[12000]["data"].(map[string]any)["count"])
				expected := []string{}
				if all {
					for start := 0; start < 12000; start += 700 {
						end := min(start+700, 12000)
						for i := end - 1; i >= start; i-- {
							expected = append(expected, fmt.Sprint(i))
						}
					}
				} else {
					for i := 11999; i >= 0; i-- {
						expected = append(expected, fmt.Sprint(i))
					}
				}
				for i, id := range expected {
					require.Equal(t, id, lines[i]["request_id"])
					if !all {
						require.EqualValues(t, i+1, lines[i]["id"])
					}
				}
			}
			// Writes after the watermark must not enter a later page, even
			// when their timestamps place them behind the current cursor.
			count := 0
			require.NoError(t, model.StreamAllLogs(context.Background(), model.LogFilter{}, common.RoleAdminUser, func(log *model.Log) error {
				if count == 0 {
					if err := db.Create(&[]model.Log{
						{UserId: 1, CreatedAt: now, RequestId: "inserted-now"},
						{UserId: 1, CreatedAt: now - 400*24*3600, RequestId: "inserted-backdated"},
					}).Error; err != nil {
						return err
					}
				}
				count++
				require.NotEqual(t, "inserted-now", log.RequestId)
				require.NotEqual(t, "inserted-backdated", log.RequestId)
				return nil
			}))
			require.Equal(t, 12000, count)

		})
	}
}

func TestLogExportReleasesConnectionBetweenBatches(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	logs := make([]model.Log, 1100)
	for i := range logs {
		logs[i] = model.Log{UserId: 1, CreatedAt: time.Now().Unix()}
	}
	require.NoError(t, db.CreateInBatches(logs, 100).Error)
	pool, err := db.DB()
	require.NoError(t, err)
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		count := 0
		done <- model.StreamUserLogs(context.Background(), 1, model.LogFilter{}, func(log *model.Log) error {
			count++
			if count == 501 {
				close(entered)
				<-resume
			}
			return nil
		})
	}()
	<-entered
	inUse := pool.Stats().InUse
	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	err = db.WithContext(writeCtx).Create(&model.Log{UserId: 2}).Error
	cancel()
	close(resume)
	require.NoError(t, <-done)
	require.Zero(t, inUse)
	require.NoError(t, err)
}

type exportDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	flushes   int
}

func (w *exportDeadlineWriter) SetWriteDeadline(d time.Time) error {
	w.deadlines = append(w.deadlines, d)
	return nil
}
func (w *exportDeadlineWriter) Flush() { w.flushes++; w.ResponseRecorder.Flush() }
func TestLogExportDeadlineReset(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	logs := make([]model.Log, 501)
	for i := range logs {
		logs[i] = model.Log{UserId: 1, CreatedAt: time.Now().Unix()}
	}
	require.NoError(t, db.CreateInBatches(logs, 100).Error)
	w := &exportDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/export", nil)
	c.Set("id", 1)
	c.Set("role", common.RoleAdminUser)
	ExportUserLogs(c)
	require.GreaterOrEqual(t, len(w.deadlines), 3)
	require.False(t, w.deadlines[0].IsZero())
	require.True(t, w.deadlines[len(w.deadlines)-1].IsZero())
	require.GreaterOrEqual(t, w.flushes, 1)
}

// Shorten only this server's deadlines, avoiding a mutable package-wide test knob.
type shortExportDeadlineWriter struct {
	http.ResponseWriter
	idle time.Duration
}

func (w shortExportDeadlineWriter) Flush()                      { w.ResponseWriter.(http.Flusher).Flush() }
func (w shortExportDeadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w shortExportDeadlineWriter) SetWriteDeadline(d time.Time) error {
	if !d.IsZero() {
		idle := w.idle
		if idle == 0 {
			idle = 100 * time.Millisecond
		}
		d = time.Now().Add(idle)
	}
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(d)
}

func TestLogExportHTTPWriteTimeoutAndKeepAlive(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&[]model.Log{{UserId: 1, CreatedAt: time.Now().Unix(), Content: strings.Repeat("x", 16*1024*1024)}, {UserId: 2, CreatedAt: time.Now().Unix()}}).Error)
	done := make(chan struct{}, 4)
	engine := gin.New()
	engine.GET("/export", func(c *gin.Context) {
		id := 1
		if c.Query("small") != "" {
			id = 2
		}
		c.Set("id", id)
		c.Set("role", common.RoleAdminUser)
		ExportUserLogs(c)
		done <- struct{}{}
	})
	engine.GET("/next", func(c *gin.Context) { time.Sleep(200 * time.Millisecond); c.String(200, "next") })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		engine.ServeHTTP(shortExportDeadlineWriter{ResponseWriter: w}, r)
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	start := time.Now()
	_, err = fmt.Fprintf(conn, "GET /export HTTP/1.1\r\nHost: test\r\n\r\n")
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled client did not release export")
	}
	require.Less(t, time.Since(start), 3*time.Second)
	require.Empty(t, logExportSlots)
	release, acquired, err := service.AcquireLogExportLock(1)
	require.NoError(t, err)
	if acquired {
		release()
	}
	require.True(t, acquired)

	keep, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer keep.Close()
	require.NoError(t, keep.SetDeadline(time.Now().Add(5*time.Second)))
	reader := bufio.NewReader(keep)
	_, err = fmt.Fprintf(keep, "GET /export?small=1 HTTP/1.1\r\nHost: test\r\n\r\n")
	require.NoError(t, err)
	response, err := http.ReadResponse(reader, nil)
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	<-done
	_, err = fmt.Fprintf(keep, "GET /next HTTP/1.1\r\nHost: test\r\n\r\n")
	require.NoError(t, err)
	response, err = http.ReadResponse(reader, nil)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "next", string(body))
}

func TestLogExportAllHistoryIgnoresRangeSetting(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	setting := operation_setting.GetLogSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	setting.MaxRangeDays = 30
	setting.UserExportEnabled = true
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Log{UserId: 1, CreatedAt: now - 400*24*3600, RequestId: "ancient"}).Error)
	for _, handler := range []gin.HandlerFunc{ExportAllLogs, ExportUserLogs} {
		for _, query := range []string{"start_timestamp=0", fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", now-401*24*3600, now)} {
			lines := callLogHandler(t, handler, 1, common.RoleCommonUser, query)
			require.Len(t, lines, 2)
			require.Equal(t, "ancient", lines[0]["request_id"])
			require.Equal(t, true, lines[1]["success"])
		}
	}
	listed := callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, fmt.Sprintf("start_timestamp=%d", now-401*24*3600))[0]
	require.Equal(t, false, listed["success"])
	require.Equal(t, "The time range cannot exceed {{days}} days", listed["message_key"])
}

func TestLogExportBatchQueryTimeoutReportsFailure(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&model.Log{UserId: 1, CreatedAt: time.Now().Unix()}).Error)
	injected := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("export_query_timeout", func(tx *gorm.DB) {
		if tx.Statement.Table != "logs" {
			return
		}
		deadline, ok := tx.Statement.Context.Deadline()
		require.True(t, ok, "every batch must have a query deadline")
		require.InDelta(t, 30, time.Until(deadline).Seconds(), 1)
		injected = true
		tx.AddError(context.DeadlineExceeded)
	}))
	lines := callLogHandler(t, ExportUserLogs, 1, common.RoleAdminUser, "")
	require.True(t, injected)
	require.Len(t, lines, 1)
	require.Equal(t, false, lines[0]["success"])
	require.Equal(t, "Failed to export logs", lines[0]["message_key"])
	require.Empty(t, logExportSlots)
}

func TestLogExportSlowContinuousReaderCompletes(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	logs := make([]model.Log, 400)
	for i := range logs {
		logs[i] = model.Log{UserId: 1, CreatedAt: time.Now().Unix(), Content: strings.Repeat("x", 64*1024)}
	}
	require.NoError(t, db.CreateInBatches(logs, 50).Error)
	engine := gin.New()
	engine.GET("/export", func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleAdminUser); ExportUserLogs(c) })
	const idle = time.Second
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		engine.ServeHTTP(shortExportDeadlineWriter{ResponseWriter: w, idle: idle}, r)
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(20*time.Second)))
	_, err = fmt.Fprintf(conn, "GET /export HTTP/1.1\r\nHost: test\r\n\r\n")
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	defer response.Body.Close()
	start := time.Now()
	buffer := make([]byte, 64*1024)
	tail := ""
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			tail += string(buffer[:n])
			if len(tail) > 1024 {
				tail = tail[len(tail)-1024:]
			}
		}
		if readErr == io.EOF {
			break
		}
		require.NoError(t, readErr, "steady reads must not hit a total-duration deadline")
		time.Sleep(5 * time.Millisecond)
	}
	require.Greater(t, time.Since(start), 2*idle)
	last := tail[strings.LastIndex(strings.TrimSpace(tail), "\n")+1:]
	var result map[string]any
	require.NoError(t, common.UnmarshalJsonStr(last, &result))
	require.Equal(t, true, result["success"])
	require.EqualValues(t, 400, result["data"].(map[string]any)["count"])
}
