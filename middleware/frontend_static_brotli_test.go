package middleware

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func brotliBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := brotli.NewWriterLevel(&compressed, brotli.BestCompression)
	_, err := writer.Write(raw)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

// newBrotliFrontendRouter serves a build where app.js, site.webmanifest and
// logo.png have .br siblings, plain.js has none and orphan.js.br has no original.
func newBrotliFrontendRouter(t *testing.T) (*gin.Engine, map[string][]byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	script := []byte(strings.Repeat("export const answer = 42;\n", 400))
	manifest := []byte(`{"name":"Ren2Hub","display":"standalone"}`)
	image := []byte("\x89PNG\r\n\x1a\nnot-really-an-image")
	files := map[string][]byte{
		"static/js/app.js":       script,
		"static/js/app.js.br":    brotliBytes(t, script),
		"static/js/plain.js":     script,
		"site.webmanifest":       manifest,
		"site.webmanifest.br":    brotliBytes(t, manifest),
		"logo.png":               image,
		"logo.png.br":            brotliBytes(t, image),
		"static/js/orphan.js.br": brotliBytes(t, script),
	}
	frontendDir := t.TempDir()
	for name, data := range files {
		target := filepath.Join(frontendDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, data, 0o644))
	}
	frontendFS := static.LocalFile(frontendDir, false)
	router := gin.New()
	router.NoRoute(ServeFrontendBrotliFiles(frontendFS), ServeFrontendFiles(frontendFS), func(c *gin.Context) {
		c.String(http.StatusTeapot, "fallback")
	})
	return router, files
}

func TestServeFrontendBrotliFilesSendsTheBuildTimeSibling(t *testing.T) {
	router, files := newBrotliFrontendRouter(t)
	plain := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "")
	require.Equal(t, http.StatusOK, plain.Code)
	require.Empty(t, plain.Header().Get("Content-Encoding"))

	response := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "br, gzip")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "br", response.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
	assert.Equal(t, plain.Header().Get("Content-Type"), response.Header().Get("Content-Type"))
	assert.Equal(t, strconv.Itoa(len(files["static/js/app.js.br"])), response.Header().Get("Content-Length"))
	assert.Equal(t, files["static/js/app.js.br"], response.Body.Bytes(), "the build-time file is sent as is")
	decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(response.Body.Bytes())))
	require.NoError(t, err)
	assert.Equal(t, files["static/js/app.js"], decoded)

	head := performFrontendRequest(router, http.MethodHead, "/static/js/app.js", "br")
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, "br", head.Header().Get("Content-Encoding"))
	assert.Equal(t, strconv.Itoa(len(files["static/js/app.js.br"])), head.Header().Get("Content-Length"))
	assert.Empty(t, head.Body.Bytes())

	// No built-in MIME type: both paths sniff the original file.
	plainManifest := performFrontendRequest(router, http.MethodGet, "/site.webmanifest", "")
	manifest := performFrontendRequest(router, http.MethodGet, "/site.webmanifest", "br")
	assert.Equal(t, "br", manifest.Header().Get("Content-Encoding"))
	assert.NotEmpty(t, manifest.Header().Get("Content-Type"))
	assert.Equal(t, plainManifest.Header().Get("Content-Type"), manifest.Header().Get("Content-Type"))

	withoutSibling := performFrontendRequest(router, http.MethodGet, "/static/js/plain.js", "br, gzip")
	assert.Equal(t, http.StatusOK, withoutSibling.Code)
	assert.Equal(t, "gzip", withoutSibling.Header().Get("Content-Encoding"))

	png := performFrontendRequest(router, http.MethodGet, "/logo.png", "br, gzip")
	assert.Equal(t, http.StatusOK, png.Code)
	assert.Empty(t, png.Header().Get("Content-Encoding"))
	assert.Equal(t, files["logo.png"], png.Body.Bytes())

	for _, missing := range []string{"/static/js/missing.js", "/static/js/orphan.js"} {
		response := performFrontendRequest(router, http.MethodGet, missing, "br, gzip")
		assert.Equal(t, http.StatusTeapot, response.Code, missing)
		assert.Equal(t, "fallback", response.Body.String(), missing)
	}
}

func TestServeFrontendBrotliFilesFollowsAcceptEncoding(t *testing.T) {
	router, _ := newBrotliFrontendRouter(t)
	for _, tc := range []struct{ acceptEncoding, encoding string }{
		{"br, gzip", "br"},
		{"gzip, deflate, BR;q=0.5", "br"},
		{"gzip;q=1.0, br ; q=0.001", "br"},
		{"gzip", "gzip"},
		{"br;q=0, gzip", "gzip"},
		{"br; q=0.000, gzip", "gzip"},
		{"br;q=invalid, gzip", "gzip"},
		{"br;q=1., gzip", "br"},
		{"br;q=2, gzip", "gzip"},
		{"br;q=-0.5, gzip", "gzip"},
		{"br;q=NaN, gzip", "gzip"},
		{"br;q=0x1p-2, gzip", "gzip"},
		{"br;q=0.0001, gzip", "gzip"},
		{"brotli, gzip", "gzip"},
		{"identity", ""},
	} {
		t.Run(tc.acceptEncoding, func(t *testing.T) {
			response := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", tc.acceptEncoding)

			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, tc.encoding, response.Header().Get("Content-Encoding"))
		})
	}
}

// The .br file is the same resource in another encoding, so preconditions and
// ranges get the answers http.FileServer gives for the original.
func TestServeFrontendBrotliFilesHonoursPreconditionsAndRanges(t *testing.T) {
	router, files := newBrotliFrontendRouter(t)
	send := func(header map[string]string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
		request.Header.Set("Accept-Encoding", "br")
		for name, value := range header {
			request.Header.Set(name, value)
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	notModified := send(map[string]string{"If-None-Match": "*"})
	assert.Equal(t, http.StatusNotModified, notModified.Code)
	assert.Empty(t, notModified.Body.Bytes())
	assert.Empty(t, notModified.Header().Get("Content-Encoding"))

	failed := send(map[string]string{"If-Match": `"another-version"`})
	assert.Equal(t, http.StatusPreconditionFailed, failed.Code)
	assert.Empty(t, failed.Header().Get("Content-Encoding"))

	ranged := send(map[string]string{"Range": "bytes=0-9"})
	assert.Equal(t, http.StatusPartialContent, ranged.Code)
	assert.Equal(t, "br", ranged.Header().Get("Content-Encoding"))
	assert.Equal(t, "10", ranged.Header().Get("Content-Length"))
	assert.Equal(t, files["static/js/app.js.br"][:10], ranged.Body.Bytes())
}

// ServeContent answers several ranges with a multipart/byteranges body that is
// not a brotli stream, so those requests get parts of the original file, with
// the same precondition answers, also when gzip is accepted.
func TestServeFrontendBrotliFilesSendsMultipleRangesFromTheOriginal(t *testing.T) {
	router, files := newBrotliFrontendRouter(t)
	send := func(header map[string]string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
		request.Header.Set("Accept-Encoding", "br, gzip")
		request.Header.Set("Range", "bytes=0-9,20-29")
		for name, value := range header {
			request.Header.Set(name, value)
		}
		router.ServeHTTP(recorder, request)
		return recorder
	}

	ranged := send(nil)
	require.Equal(t, http.StatusPartialContent, ranged.Code)
	assert.Empty(t, ranged.Header().Get("Content-Encoding"))
	mediaType, params, err := mime.ParseMediaType(ranged.Header().Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/byteranges", mediaType)
	reader := multipart.NewReader(ranged.Body, params["boundary"])
	original := files["static/js/app.js"]
	for _, want := range [][]byte{original[0:10], original[20:30]} {
		part, err := reader.NextPart()
		require.NoError(t, err)
		body, err := io.ReadAll(part)
		require.NoError(t, err)
		assert.Equal(t, want, body)
	}
	_, err = reader.NextPart()
	assert.ErrorIs(t, err, io.EOF)

	failed := send(map[string]string{"If-Match": `"another-version"`})
	assert.Equal(t, http.StatusPreconditionFailed, failed.Code)
	assert.Empty(t, failed.Header().Get("Content-Encoding"))

	notModified := send(map[string]string{"If-None-Match": "*"})
	assert.Equal(t, http.StatusNotModified, notModified.Code)
	assert.Empty(t, notModified.Body.Bytes())
}
