package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testWebAssets(index string) WebAssets {
	return WebAssets{
		NextBuildFS: fstest.MapFS{
			"frontend/embed-dist/index.html":    {Data: []byte(index)},
			"frontend/embed-dist/assets/app.js": {Data: []byte("next-asset")},
			"frontend/embed-dist/logo.png":      {Data: []byte("logo")},
		},
		NextIndexPage: []byte(index),
	}
}

func serveWebRequest(t *testing.T, assets WebAssets, requestPath string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetWebRouter(engine, assets)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
	return recorder
}

func TestVueWebRouting(t *testing.T) {
	assets := testWebAssets("next-index")
	for _, test := range []struct {
		path string
		code int
		body string
	}{
		{"/", http.StatusTemporaryRedirect, ""},
		{"/models", http.StatusTemporaryRedirect, ""},
		{"/next/console/keys", http.StatusOK, "next-index"},
		{"/next/assets/app.js", http.StatusOK, "next-asset"},
		{"/logo.png", http.StatusOK, "logo"},
		{"/playground", http.StatusNotFound, ""},
		{"/next/chat/123", http.StatusNotFound, ""},
		{"/console/chat-presets", http.StatusNotFound, ""},
		{"/api/missing", http.StatusNotFound, ""},
		{"/pg/missing", http.StatusNotFound, ""},
	} {
		recorder := serveWebRequest(t, assets, test.path)
		require.Equal(t, test.code, recorder.Code, test.path)
		if test.body != "" {
			require.Equal(t, test.body, recorder.Body.String(), test.path)
		}
	}
}

func TestVueWebPreservesQueryOnRedirect(t *testing.T) {
	recorder := serveWebRequest(t, testWebAssets("next-index"), "/usage-logs?tab=drawing")
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Equal(t, "/next/usage-logs?tab=drawing", recorder.Header().Get("Location"))
}

func TestVuePlaceholderIsUnavailable(t *testing.T) {
	index := `<meta name="ren2hub-next-build" content="placeholder">`
	recorder := serveWebRequest(t, testWebAssets(index), "/next/")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
