package router

import (
	"embed"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed frontend/embed-dist/brotli-probe.js frontend/embed-dist/brotli-probe.js.br
var brotliProbeAssets embed.FS

func TestWebRouterSendsBrotliBeforeTheGzipPath(t *testing.T) {
	previous := common.GlobalWebRateLimitEnable
	common.GlobalWebRateLimitEnable = false
	t.Cleanup(func() { common.GlobalWebRateLimitEnable = previous })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetWebRouter(engine, WebAssets{BuildFS: brotliProbeAssets, IndexPage: []byte("probe index")}, func(c *gin.Context) { c.Next() })
	want, err := brotliProbeAssets.ReadFile("frontend/embed-dist/brotli-probe.js")
	require.NoError(t, err)
	precompressed, err := brotliProbeAssets.ReadFile("frontend/embed-dist/brotli-probe.js.br")
	require.NoError(t, err)
	get := func(acceptEncoding string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/brotli-probe.js", nil)
		request.Header.Set("Accept-Encoding", acceptEncoding)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}

	br := get("gzip, br")
	gzip := get("gzip")

	require.Equal(t, http.StatusOK, br.Code)
	assert.Equal(t, "br", br.Header().Get("Content-Encoding"))
	assert.NotEmpty(t, br.Header().Get("Cache-Control"))
	assert.Equal(t, gzip.Header().Get("Cache-Control"), br.Header().Get("Cache-Control"), "middleware.Cache runs before both paths")
	assert.Equal(t, precompressed, br.Body.Bytes())
	decoded, err := io.ReadAll(brotli.NewReader(br.Body))
	require.NoError(t, err)
	assert.Equal(t, want, decoded)
	require.Equal(t, http.StatusOK, gzip.Code)
	assert.Equal(t, "gzip", gzip.Header().Get("Content-Encoding"))
}
