package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func updateTestBuild(t *testing.T, version string) {
	t.Helper()
	v, kind := common.Version, common.BuildType
	common.Version, common.BuildType = version, "release"
	t.Cleanup(func() { common.Version, common.BuildType = v, kind })
}

func TestSystemUpdateCheckCache(t *testing.T) {
	updateTestBuild(t, "v1.0.9")
	var calls atomic.Int32
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/releases/latest" || unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v1.0.10","name":"Release","published_at":"2026-11-01","html_url":"https://github.com/DayFliggy/Ren2Hub","body":"Changes"}`)
	}))
	defer server.Close()
	s := NewSystemUpdateService(server.URL, server.Client())
	ctx := t.Context()
	unavailable.Store(true)
	_, err := s.CheckUpdate(ctx, false)
	require.ErrorIs(t, err, ErrSystemRelease)
	unavailable.Store(false)
	info, err := s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.False(t, info.Cached)
	require.Equal(t, "v1.0.10", info.LatestVersion)
	require.Equal(t, "Release", info.ReleaseInfo.Name)

	s.cachedAt = time.Now().Add(-19 * time.Minute)
	info, err = s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.EqualValues(t, 2, calls.Load())
	s.cachedAt = time.Now().Add(-20 * time.Minute)
	info, err = s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.False(t, info.Cached)
	require.EqualValues(t, 3, calls.Load())
	unavailable.Store(true)
	info, err = s.CheckUpdate(ctx, true)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.NotEmpty(t, info.Warning)
	require.EqualValues(t, 4, calls.Load())
	_, err = s.PerformUpdate(ctx)
	require.ErrorIs(t, err, ErrSystemRelease)
}

func TestSystemUpdateCheckWithoutRelease(t *testing.T) {
	updateTestBuild(t, "v1.0.1")
	var published atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" || !published.Load() {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v1.0.2","name":"Release","published_at":"2026-11-01","html_url":"https://github.com/DayFliggy/Ren2Hub","body":""}`)
	}))
	defer server.Close()
	s := NewSystemUpdateService(server.URL, server.Client())
	ctx := t.Context()
	checkNoRelease := func() {
		t.Helper()
		info, err := s.CheckUpdate(ctx, true)
		require.NoError(t, err)
		data, err := common.Marshal(info)
		require.NoError(t, err)
		require.JSONEq(t, `{"current_version":"v1.0.1","latest_version":"","build_type":"release","has_update":false,"cached":false}`, string(data))
	}
	checkNoRelease()
	_, err := s.PerformUpdate(ctx)
	require.ErrorIs(t, err, ErrSystemRelease)

	// A release that is withdrawn later is not served from the cache.
	published.Store(true)
	info, err := s.CheckUpdate(ctx, true)
	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	published.Store(false)
	checkNoRelease()
	info, err = s.CheckUpdate(ctx, false)
	require.NoError(t, err)
	require.Empty(t, info.LatestVersion)
}

func TestSystemUpdateRollbackVersions(t *testing.T) {
	updateTestBuild(t, "v1.2.20")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/releases?per_page=100" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `[{"tag_name":"v1.2.3"},{"tag_name":"v1.2.10"},{"tag_name":"1.2.10"},{"tag_name":"v1.2.20"},{"tag_name":"v1.2.19","prerelease":true},{"tag_name":"v1.2.15","draft":true},{"tag_name":"v1.2.4-rc1"},{"tag_name":"v1.2.1"},{"tag_name":"v1.2.9"},{"tag_name":"v1.3.0"}]`)
	}))
	defer server.Close()
	s := NewSystemUpdateService(server.URL, server.Client())
	versions, err := s.ListRollbackVersions(t.Context())
	require.NoError(t, err)
	require.Equal(t, []RollbackVersion{{Version: "1.2.10"}, {Version: "1.2.9"}, {Version: "1.2.3"}}, versions)
	require.ErrorIs(t, s.RollbackToVersion(t.Context(), "1.2.1"), ErrSystemRollbackNotFound)
	for _, bad := range []string{"", "1", "1.2.3-rc1", "1.2/3", "01.2.3"} {
		require.ErrorIs(t, s.RollbackToVersion(t.Context(), bad), ErrSystemInvalidVersion)
	}
}

type updateTestAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type updateTestServer struct {
	assets       []updateTestAsset
	archive      []byte
	checksum     string
	redirect     string
	sumsRedirect string
}
type updateTestRoundTripper func(*http.Request) (*http.Response, error)

func (f updateTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func updateTestArchive(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	writer := tar.NewWriter(gz)
	for i := range entries {
		header := &entries[i]
		var content []byte
		if header.Typeflag == tar.TypeReg {
			content = []byte("new binary")
			header.Size = int64(len(content))
		}
		require.NoError(t, writer.WriteHeader(header))
		if len(content) > 0 {
			_, err := writer.Write(content)
			require.NoError(t, err)
		}
	}
	require.NoError(t, writer.Close())
	require.NoError(t, gz.Close())
	return b.Bytes()
}

func (f *updateTestServer) sign() {
	f.checksum = fmt.Sprintf("%x  %s\n", sha256.Sum256(f.archive), f.assets[0].Name)
}

func newUpdateTestServer(t *testing.T) (*SystemUpdateService, *updateTestServer) {
	t.Helper()
	name := "ren2hub_1.0.10_linux_amd64.tar.gz"
	f := &updateTestServer{
		assets:  []updateTestAsset{{name, "https://github.com/archive", 0}, {"checksums.txt", "https://objects.githubusercontent.com/sums", 0}},
		archive: updateTestArchive(t, tar.Header{Name: "ren2hub", Typeflag: tar.TypeReg}),
	}
	f.sign()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest", "/releases":
			release := map[string]any{"tag_name": "v1.0.10", "assets": f.assets}
			var value any = release
			if r.URL.Path == "/releases" {
				value = []any{release}
			}
			data, err := common.Marshal(value)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			_, _ = w.Write(data)
		case "/archive":
			if f.redirect != "" {
				http.Redirect(w, r, f.redirect, http.StatusFound)
				return
			}
			_, _ = w.Write(f.archive)
		case "/sums":
			if f.sumsRedirect != "" {
				http.Redirect(w, r, f.sumsRedirect, http.StatusFound)
				return
			}
			_, _ = io.WriteString(w, f.checksum)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirected-archive":
			_, _ = w.Write(f.archive)
		case "/redirected-sums":
			_, _ = io.WriteString(w, f.checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(cdn.Close)
	cdnURL, err := url.Parse(cdn.URL)
	require.NoError(t, err)
	local, err := url.Parse(server.URL)
	require.NoError(t, err)
	transport := server.Client().Transport
	client := &http.Client{Transport: updateTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		dest := local
		if r.URL.Host == "release-assets.githubusercontent.com" {
			dest = cdnURL
		}
		req := r.Clone(r.Context())
		req.URL.Scheme, req.URL.Host = dest.Scheme, dest.Host
		return transport.RoundTrip(req)
	})}
	s := NewSystemUpdateService(server.URL, client)
	s.executable = filepath.Join(t.TempDir(), "ren2hub")
	require.NoError(t, os.WriteFile(s.executable, []byte("old binary"), 0755))
	return s, f
}

func TestSystemUpdateRejectInvalidAssets(t *testing.T) {
	updateTestBuild(t, "v1.0.1")
	for _, tt := range []struct {
		name   string
		mutate func(*testing.T, *updateTestServer)
	}{
		{"wrong asset", func(t *testing.T, f *updateTestServer) { f.assets[0].Name += ".copy" }},
		{"missing sums file", func(t *testing.T, f *updateTestServer) { f.assets = f.assets[:1] }},
		{"no matching entry", func(t *testing.T, f *updateTestServer) {
			f.checksum = strings.ReplaceAll(f.checksum, "linux", "darwin")
		}},
		{"wrong sum", func(t *testing.T, f *updateTestServer) {
			f.checksum = strings.Repeat("1", 64) + "  " + f.assets[0].Name
		}},
		{"HTTP", func(t *testing.T, f *updateTestServer) { f.assets[0].URL = "http://github.com/archive" }},
		{"wrong host", func(t *testing.T, f *updateTestServer) { f.assets[1].URL = "https://untrusted.test/sums" }},
		{"oversize", func(t *testing.T, f *updateTestServer) { f.assets[0].Size = 500*1024*1024 + 1 }},
		{"traversal after executable", func(t *testing.T, f *updateTestServer) {
			f.archive = updateTestArchive(t, tar.Header{Name: "ren2hub", Typeflag: tar.TypeReg}, tar.Header{Name: "../x", Typeflag: tar.TypeReg})
			f.sign()
		}},
		{"symlink", func(t *testing.T, f *updateTestServer) {
			f.archive = updateTestArchive(t, tar.Header{Name: "ren2hub", Typeflag: tar.TypeSymlink, Linkname: "LICENSE"})
			f.sign()
		}},
		{"other file", func(t *testing.T, f *updateTestServer) {
			f.archive = updateTestArchive(t, tar.Header{Name: "other", Typeflag: tar.TypeReg})
			f.sign()
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, f := newUpdateTestServer(t)
			tt.mutate(t, f)
			_, err := s.PerformUpdate(t.Context())
			require.ErrorIs(t, err, ErrSystemRelease)
			require.FileExists(t, s.executable)
			data, err := os.ReadFile(s.executable)
			require.NoError(t, err)
			require.Equal(t, "old binary", string(data))
			require.NoFileExists(t, s.executable+".backup")
		})
	}
	t.Run("stream limit without content length", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "12345")
		}))
		defer server.Close()
		local, err := url.Parse(server.URL)
		require.NoError(t, err)
		transport := server.Client().Transport
		client := &http.Client{Transport: updateTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			req := r.Clone(r.Context())
			req.URL.Scheme, req.URL.Host = local.Scheme, local.Host
			return transport.RoundTrip(req)
		})}
		service := NewSystemUpdateService(server.URL, client)
		err = service.download(t.Context(), systemAsset{URL: "https://github.com/archive"}, io.Discard, 4)
		require.ErrorIs(t, err, ErrSystemRelease)
	})

}

func TestSystemUpdateReplaceAndRollback(t *testing.T) {
	for _, action := range []string{"update", "rollback", "already latest", "source"} {
		t.Run(action, func(t *testing.T) {
			updateTestBuild(t, map[string]string{"update": "v1.0.9", "rollback": "v1.0.11", "already latest": "v1.0.10", "source": "v1.0.9"}[action])
			s, _ := newUpdateTestServer(t)
			switch action {
			case "update":
				info, err := s.PerformUpdate(t.Context())
				require.NoError(t, err)
				require.True(t, info.HasUpdate)
			case "rollback":
				require.NoError(t, s.RollbackToVersion(t.Context(), "1.0.10"))
			case "already latest":
				info, err := s.PerformUpdate(t.Context())
				require.NoError(t, err)
				require.False(t, info.HasUpdate)
				require.NoFileExists(t, s.executable+".backup")
				return
			case "source":
				common.BuildType = "source"
				_, err := s.PerformUpdate(t.Context())
				require.ErrorIs(t, err, ErrSystemSourceBuild)
				require.ErrorIs(t, s.RollbackToVersion(t.Context(), "1.0.1"), ErrSystemSourceBuild)
				require.NoFileExists(t, s.executable+".backup")
				return
			}
			b, err := os.ReadFile(s.executable)
			require.NoError(t, err)
			require.Equal(t, "new binary", string(b))
			b, err = os.ReadFile(s.executable + ".backup")
			require.NoError(t, err)
			require.Equal(t, "old binary", string(b))
			info, err := os.Stat(s.executable)
			require.NoError(t, err)
			require.NotZero(t, info.Mode()&0111)
		})
	}
}

func TestSystemUpdateRestoreOnReplaceFailure(t *testing.T) {
	s, _ := newUpdateTestServer(t)
	err := replaceSystemBinary(s.executable, filepath.Join(filepath.Dir(s.executable), "absent"))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrSystemRelease)
	require.ErrorIs(t, err, ErrSystemReplace)
	b, err := os.ReadFile(s.executable)
	require.NoError(t, err)
	require.Equal(t, "old binary", string(b))
}

func checkReleaseRedirect(t *testing.T, rollback bool) {
	t.Helper()
	current := "v1.0.9"
	if rollback {
		current = "v1.0.11"
	}
	updateTestBuild(t, current)
	s, f := newUpdateTestServer(t)
	f.assets[1].URL = "https://github.com/sums"
	f.redirect = "https://release-assets.githubusercontent.com/redirected-archive"
	f.sumsRedirect = "https://release-assets.githubusercontent.com/redirected-sums"
	if rollback {
		require.NoError(t, s.RollbackToVersion(t.Context(), "1.0.10"))
	} else {
		info, err := s.PerformUpdate(t.Context())
		require.NoError(t, err)
		require.True(t, info.HasUpdate)
	}
	binary, err := os.ReadFile(s.executable)
	require.NoError(t, err)
	require.Equal(t, "new binary", string(binary))
	backup, err := os.ReadFile(s.executable + ".backup")
	require.NoError(t, err)
	require.Equal(t, "old binary", string(backup))
}

func TestSystemUpdateFollowsAssetRedirect(t *testing.T)   { checkReleaseRedirect(t, false) }
func TestSystemRollbackFollowsAssetRedirect(t *testing.T) { checkReleaseRedirect(t, true) }

func TestSystemUpdateStageErrors(t *testing.T) {
	t.Run("deadline while unpacking", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancel()
		archive := updateTestArchive(t, tar.Header{Name: "ren2hub", Typeflag: tar.TypeReg})
		err := extractSystemBinary(ctx, bytes.NewReader(archive), filepath.Join(t.TempDir(), "ren2hub"))
		require.ErrorIs(t, err, ErrSystemRelease)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
	t.Run("staging directory unavailable", func(t *testing.T) {
		updateTestBuild(t, "v1.0.9")
		s, _ := newUpdateTestServer(t)
		s.executable = filepath.Join(s.executable, "ren2hub")
		_, err := s.PerformUpdate(t.Context())
		var pathErr *os.PathError
		require.ErrorAs(t, err, &pathErr)
		require.NotErrorIs(t, err, ErrSystemReplace)
		require.NotErrorIs(t, err, ErrSystemRelease)
	})
}
