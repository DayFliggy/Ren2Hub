package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The fake only replaces the release download and file replacement boundary.
type fakeSystemReleases struct {
	info     *service.SystemUpdateInfo
	versions []service.RollbackVersion
	err      error
	update   func(context.Context)
	rollback func(context.Context, string)
	forced   bool
}

func (f *fakeSystemReleases) CheckUpdate(_ context.Context, force bool) (*service.SystemUpdateInfo, error) {
	f.forced = force
	return f.info, f.err
}
func (f *fakeSystemReleases) ListRollbackVersions(context.Context) ([]service.RollbackVersion, error) {
	return f.versions, f.err
}
func (f *fakeSystemReleases) PerformUpdate(ctx context.Context) (*service.SystemUpdateInfo, error) {
	if f.update != nil {
		f.update(ctx)
	}
	return f.info, f.err
}
func (f *fakeSystemReleases) RollbackToVersion(ctx context.Context, version string) error {
	if f.rollback != nil {
		f.rollback(ctx, version)
	}
	return f.err
}

func systemRequest(ctx context.Context, handler gin.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	out := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(out)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	c.Set("role", 100)
	handler(c)
	return out
}

func TestSystemUpdateHandlerContracts(t *testing.T) {
	setupAccessTokenAudit(t)
	f := &fakeSystemReleases{
		info:     &service.SystemUpdateInfo{CurrentVersion: "v1.0.1", LatestVersion: "v1.0.2", HasUpdate: true, BuildType: "release", ReleaseInfo: &service.SystemReleaseInfo{Name: "v1.0.2", PublishedAt: "2026-01-01", Body: "notes", HTMLURL: "https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.2"}},
		versions: []service.RollbackVersion{{Version: "1.0.0", PublishedAt: "2025-12-01", HTMLURL: "https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.0"}},
	}
	h := &systemUpdateHandler{releases: f}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checkDeadline := func(ctx context.Context) {
		require.NoError(t, ctx.Err())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(15*time.Minute), deadline, time.Second)
	}
	f.update = checkDeadline
	f.rollback = func(ctx context.Context, version string) {
		checkDeadline(ctx)
		require.Equal(t, "1.0.0", version)
	}
	for _, tc := range []struct {
		handler                  gin.HandlerFunc
		method, path, body, data string
	}{
		{h.CheckUpdates, "GET", "/check-updates?force=true", "", `{"current_version":"v1.0.1","latest_version":"v1.0.2","has_update":true,"build_type":"release","cached":false,"release_info":{"name":"v1.0.2","body":"notes","published_at":"2026-01-01","html_url":"https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.2"}}`},
		{h.RollbackVersions, "GET", "/rollback-versions", "", `{"versions":[{"version":"1.0.0","published_at":"2025-12-01","html_url":"https://github.com/DayFliggy/Ren2Hub/releases/tag/v1.0.0"}]}`},
		{h.Update, "POST", "/update", "", `{"need_restart":true}`},
		{h.Rollback, "POST", "/rollback", `{"version":"1.0.0"}`, `{"need_restart":true,"version":"1.0.0"}`},
	} {
		res := systemRequest(ctx, tc.handler, tc.method, tc.path, tc.body)
		require.Equal(t, 200, res.Code)
		require.JSONEq(t, `{"success":true,"message":"","data":`+tc.data+`}`, res.Body.String())
	}
	require.True(t, f.forced)
	f.info.HasUpdate = false
	f.info.LatestVersion = "v1.0.1"
	res := systemRequest(ctx, h.Update, "POST", "/update", "")
	require.JSONEq(t, `{"success":true,"message":"","data":{"already_up_to_date":true,"current_version":"v1.0.1","latest_version":"v1.0.1"}}`, res.Body.String())
	var actions []string
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("success = ?", true).Order("id").Pluck("action", &actions).Error)
	require.Equal(t, []string{"system.update", "system.rollback", "system.update"}, actions)
}

func TestSystemUpdateHandlerErrors(t *testing.T) {
	f := &fakeSystemReleases{}
	h := &systemUpdateHandler{releases: f}
	for _, tc := range []struct {
		handler gin.HandlerFunc
		status  int
		err     error
		body    string
	}{
		{h.Update, 400, service.ErrSystemSourceBuild, ""},
		{h.Rollback, 400, service.ErrSystemSourceBuild, `{"version":"1.0.0"}`},
		{h.Rollback, 400, nil, "{"},
		{h.Rollback, 400, service.ErrSystemInvalidVersion, `{"version":"bad"}`},
		{h.Rollback, 404, service.ErrSystemRollbackNotFound, `{"version":"1.0.0"}`},
		{h.CheckUpdates, 502, service.ErrSystemRelease, ""},
		{h.RollbackVersions, 502, service.ErrSystemRelease, ""},
		{h.Update, 502, service.ErrSystemRelease, ""},
		{h.Rollback, 502, service.ErrSystemRelease, `{"version":"1.0.0"}`},
		{h.Update, 500, errors.New("private executable path"), ""},
		{h.Rollback, 500, errors.New("private executable path"), `{"version":"1.0.0"}`},
	} {
		f.err = tc.err
		res := systemRequest(context.Background(), tc.handler, "POST", "/", tc.body)
		require.Equal(t, tc.status, res.Code)
		require.Contains(t, res.Body.String(), `"success":false`)
		require.NotContains(t, res.Body.String(), "private executable path")
	}
}

func TestSystemUpdateMutationsDoNotQueue(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := &fakeSystemReleases{err: service.ErrSystemRelease, update: func(context.Context) { close(entered); <-release }}
	h := &systemUpdateHandler{releases: f}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- systemRequest(context.Background(), h.Update, "POST", "/update", "") }()
	defer func() { close(release); <-done }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("update did not start")
	}
	for _, handler := range []gin.HandlerFunc{h.Update, h.Rollback, h.Restart} {
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() { result <- systemRequest(context.Background(), handler, "POST", "/", `{"version":"1.0.0"}`) }()
		select {
		case res := <-result:
			require.Equal(t, 409, res.Code)
		case <-time.After(time.Second):
			t.Fatal("second mutation queued behind update")
		}
	}
}

func TestSystemUpdateRestartAfterResponse(t *testing.T) {
	setupAccessTokenAudit(t)
	sent := make(chan time.Time, 1)
	h := &systemUpdateHandler{restart: func() error { sent <- time.Now(); return nil }}
	start := time.Now()
	res := systemRequest(context.Background(), h.Restart, "POST", "/restart", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.True(t, res.Flushed)
	require.JSONEq(t, `{"success":true,"message":"","data":{"restarting":true}}`, res.Body.String())
	require.Equal(t, 409, systemRequest(context.Background(), h.Restart, "POST", "/restart", "").Code)
	select {
	case when := <-sent:
		require.GreaterOrEqual(t, when.Sub(start), 500*time.Millisecond)
	case <-time.After(3 * time.Second):
		t.Fatal("signal was not sent after flushing response")
	}
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("action = ? AND success = ?", "system.restart", true).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
