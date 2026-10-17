package main

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/router"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Run after prepare-frontend-embed to test what the binary actually embeds.
func TestProductionEmbeddedAssetGraph(t *testing.T) {
	if bytes.Contains(frontendIndexPage, []byte(`content="placeholder"`)) {
		t.Skip("build frontend and prepare embed assets for production verification")
	}
	embedded, err := fs.Sub(frontendBuildFS, "frontend/embed-dist")
	require.NoError(t, err)
	content, err := fs.ReadFile(embedded, ".vite/manifest.json")
	require.NoError(t, err)
	var manifest map[string]struct {
		File           string   `json:"file"`
		CSS            []string `json:"css"`
		Assets         []string `json:"assets"`
		Imports        []string `json:"imports"`
		DynamicImports []string `json:"dynamicImports"`
	}
	require.NoError(t, common.Unmarshal(content, &manifest))
	require.NotEmpty(t, manifest)
	for name, entry := range manifest {
		for _, dependency := range append(entry.Imports, entry.DynamicImports...) {
			require.Contains(t, manifest, dependency, name)
		}
		for _, file := range append(append([]string{entry.File}, entry.CSS...), entry.Assets...) {
			data, err := fs.ReadFile(embedded, file)
			require.NoError(t, err, "%s references missing embedded asset %s", name, file)
			require.NotEmpty(t, data, file)
		}
	}
}

func TestProductionEmbeddedModelPagesAndFiles(t *testing.T) {
	if bytes.Contains(frontendIndexPage, []byte(`content="placeholder"`)) {
		t.Skip("build frontend and prepare embed assets for production verification")
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	router.SetApiRouter(engine)
	router.SetRelayRouter(engine)
	router.SetWebRouter(engine, router.WebAssets{
		BuildFS: frontendBuildFS, IndexPage: frontendIndexPage,
	}, func(c *gin.Context) { c.Next() })
	server := httptest.NewServer(engine)
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// The /models SPA route shares its path with the embedded model icons.
	// A directory must reach the SPA fallback instead of FileServer redirects.
	for _, path := range []string{"/models", "/models/", "/models/pricing", "/users", "/system-settings/security"} {
		t.Run(path, func(t *testing.T) {
			response, err := client.Get(server.URL + path)
			require.NoError(t, err)
			defer response.Body.Close()
			// Gin may canonicalize a trailing slash before the web fallback.
			// Permit that single native redirect, then require the SPA response.
			if path == "/models/" && response.StatusCode == http.StatusMovedPermanently {
				require.Equal(t, "/models", response.Header.Get("Location"))
				response.Body.Close()
				response, err = client.Get(server.URL + "/models")
				require.NoError(t, err)
				defer response.Body.Close()
			}
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Empty(t, response.Header.Get("Location"))
			require.Equal(t, "no-cache", response.Header.Get("Cache-Control"))
			require.Equal(t, frontendIndexPage, body)
		})
	}

	t.Run("model icon remains a static file", func(t *testing.T) {
		want, err := frontendBuildFS.ReadFile("frontend/embed-dist/models/openai.svg")
		require.NoError(t, err)
		response, err := client.Get(server.URL + "/models/openai.svg")
		require.NoError(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Contains(t, response.Header.Get("Content-Type"), "image/svg+xml")
		require.Equal(t, want, body)
	})

	for _, path := range []string{"/api/models", "/v1/models"} {
		t.Run(path+" keeps native authentication", func(t *testing.T) {
			response, err := client.Get(server.URL + path)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusUnauthorized, response.StatusCode)
			require.Contains(t, response.Header.Get("Content-Type"), "application/json")
		})
	}
}
