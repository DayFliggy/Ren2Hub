package router

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// This wrapper is below Gin and every production middleware. It only shortens
// deadlines that the actual middleware chain successfully forwards.
type exportRouteDeadlineWriter struct {
	http.ResponseWriter
	calls *atomic.Int32
}

func (w exportRouteDeadlineWriter) SetWriteDeadline(d time.Time) error {
	if !d.IsZero() {
		w.calls.Add(1)
		d = time.Now().Add(200 * time.Millisecond)
	}
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(d)
}
func (w exportRouteDeadlineWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

func TestLogExportRealRoutesStreamAndStopStalledClients(t *testing.T) {
	for index, tc := range []struct {
		name, path string
		pat        bool
	}{
		{"admin-session", "/api/log/export", false},
		{"self-session", "/api/log/self/export", false},
		{"admin-pat", "/api/log/export", true},
		{"self-pat", "/api/log/self/export", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, db := setupDashboardSystemStatusRouter(t)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			previous := operation_setting.GetLogSetting().UserExportEnabled
			operation_setting.GetLogSetting().UserExportEnabled = true
			t.Cleanup(func() { operation_setting.GetLogSetting().UserExportEnabled = previous })
			user, bearer := createSystemStatusSession(t, db, 93200+index, "export-"+tc.name, common.RoleAdminUser)
			if tc.pat {
				bearer = model.AccessTokenPrefix + tc.name
				token := model.UserAccessToken{UserId: user.Id, Name: tc.name, TokenHash: model.AccessTokenFingerprint(bearer), ExpiresAt: time.Now().Add(time.Hour).Unix()}
				require.NoError(t, token.SetScopes([]string{"usage:read", "log:read"}))
				require.NoError(t, db.Create(&token).Error)
			}
			logs := make([]model.Log, 600)
			for i := range logs {
				logs[i] = model.Log{UserId: user.Id, CreatedAt: time.Now().Unix(), Content: strings.Repeat("x", 32*1024), RequestId: fmt.Sprint(i)}
			}
			require.NoError(t, db.CreateInBatches(logs, 50).Error)
			var calls atomic.Int32
			done := make(chan struct{}, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				engine.ServeHTTP(exportRouteDeadlineWriter{w, &calls}, r)
				done <- struct{}{}
			}))
			defer server.Close()
			dial := func() net.Conn {
				conn, err := net.Dial("tcp", server.Listener.Addr().String())
				require.NoError(t, err)
				require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
				_, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: test\r\nAuthorization: Bearer %s\r\nAccept-Encoding: gzip\r\n\r\n", tc.path, bearer)
				require.NoError(t, err)
				return conn
			}
			// The real gzip negotiation must leave both export routes uncompressed.
			conn := dial()
			defer conn.Close()
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			require.NoError(t, err)
			require.Equal(t, 200, response.StatusCode)
			require.Empty(t, response.Header.Get("Content-Encoding"))
			require.Contains(t, response.Header.Get("Content-Type"), "application/x-ndjson")
			reader := bufio.NewReader(response.Body)
			line, err := reader.ReadString('\n')
			require.NoError(t, err)
			require.Contains(t, line, "request_id")
			select {
			case <-done:
				t.Fatal("export completed before the client read beyond the first row")
			default:
			}
			var last string
			for {
				line, err = reader.ReadString('\n')
				if line != "" {
					last = line
				}
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			require.Contains(t, last, "\"success\":true")
			require.Contains(t, last, "\"count\":600")
			require.NoError(t, response.Body.Close())
			require.NoError(t, conn.Close())
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("completed export did not return")
			}

			calls.Store(0)
			stalled := dial()
			start := time.Now()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				stalled.Close()
				t.Fatal("stalled export did not terminate")
			}
			require.Greater(t, calls.Load(), int32(0), "deadline must traverse gzip and PAT audit middleware")
			require.Greater(t, time.Since(start), 100*time.Millisecond, "must reach a stalled write, not fail authentication or preflight")
			require.Less(t, time.Since(start), 2*time.Second)
			require.NoError(t, stalled.Close())
			release, acquired, err := service.AcquireLogExportLock(user.Id)
			require.NoError(t, err)
			if acquired {
				release()
			}
			require.True(t, acquired)
			if tc.pat {
				var count int64
				require.NoError(t, db.Model(&model.AuditLog{}).Where("user_id = ? AND category = ?", user.Id, model.AuditCategoryAccessToken).Count(&count).Error)
				require.GreaterOrEqual(t, count, int64(2), "actual PAT audit wrapper must execute")
			}
			retry, err := http.NewRequest("GET", server.URL+tc.path+"?request_id=not-present", nil)
			require.NoError(t, err)
			retry.Header.Set("Authorization", "Bearer "+bearer)
			retry.Header.Set("Accept-Encoding", "gzip")
			retried, err := server.Client().Do(retry)
			require.NoError(t, err)
			retryBody, err := io.ReadAll(retried.Body)
			require.NoError(t, err)
			require.NoError(t, retried.Body.Close())
			require.Contains(t, string(retryBody), "\"success\":true", "a stalled export must release both locks and slots")
			require.Contains(t, string(retryBody), "\"count\":0")
			// An adjacent API route still negotiates gzip.
			request, _ := http.NewRequest("GET", server.URL+"/api/notice", nil)
			request.Header.Set("Accept-Encoding", "gzip")
			result, err := server.Client().Do(request)
			require.NoError(t, err)
			require.Equal(t, "gzip", result.Header.Get("Content-Encoding"))
			_, err = io.Copy(io.Discard, result.Body)
			require.NoError(t, err)
			require.NoError(t, result.Body.Close())
		})
	}
}
