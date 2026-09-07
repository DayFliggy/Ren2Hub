package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testWebAssets(index string) WebAssets {
	return WebAssets{
		BuildFS: fstest.MapFS{
			"frontend/embed-dist/index.html":                                   {Data: []byte(index)},
			"frontend/embed-dist/assets/app.js":                                {Data: []byte("asset")},
			"frontend/embed-dist/assets/app-AbCd1234.js":                       {Data: []byte("hashed-asset")},
			"frontend/embed-dist/assets/_plugin-vue_export-helper-BDNMzG2s.js": {Data: []byte("helper")},
			"frontend/embed-dist/logo.png":                                     {Data: []byte("logo")},
			"frontend/embed-dist/.vite/manifest.json":                          {Data: []byte(`{"private":"manifest"}`)},
		},
		IndexPage: []byte(index),
	}
}

func newWebTestRouter(t *testing.T, assets WebAssets, frontendBaseURL string, master bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_BASE_URL", frontendBaseURL)
	previousMaster := common.IsMasterNode
	previousWebRateLimit := common.GlobalWebRateLimitEnable
	previousAPIRateLimit := common.GlobalApiRateLimitEnable
	common.IsMasterNode = master
	common.GlobalWebRateLimitEnable = false
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() {
		common.IsMasterNode = previousMaster
		common.GlobalWebRateLimitEnable = previousWebRateLimit
		common.GlobalApiRateLimitEnable = previousAPIRateLimit
	})
	engine := gin.New()
	SetRouter(engine, assets)
	return engine
}

func serveWebRequest(engine *gin.Engine, method, requestPath string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(method, requestPath, nil))
	return recorder
}

func TestVueWebRouting(t *testing.T) {
	engine := newWebTestRouter(t, testWebAssets("vue-index"), "", true)
	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/", "vue-index"},
		{http.MethodHead, "/", ""},
		{http.MethodGet, "/models", "vue-index"},
		{http.MethodGet, "/models/claude-3.7-sonnet", "vue-index"},
		{http.MethodGet, "/console/keys", "vue-index"},
		{http.MethodHead, "/console/keys", ""},
		{http.MethodGet, "/wallet?pay=success", "vue-index"},
		{http.MethodGet, "/oauth/github?code=return-code", "vue-index"},
		{http.MethodGet, "/index.html", "vue-index"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			recorder := serveWebRequest(engine, test.method, test.path)
			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, test.body, recorder.Body.String())
			assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
			assert.Contains(t, recorder.Header().Get("Content-Type"), "text/html")
		})
	}
}

func TestVueWebFallbackBoundaries(t *testing.T) {
	for _, frontendBaseURL := range []string{"", "https://frontend.example"} {
		t.Run(frontendBaseURL, func(t *testing.T) {
			engine := newWebTestRouter(t, testWebAssets("vue-index"), frontendBaseURL, false)
			for _, path := range []string{
				"/next", "/next/", "/next/keys", "/next/usage-logs?tab=drawing&model=a%2Fb",
				"/next/assets/app.js?v=2", "/next/logo.png", "/next//example.com", "/next/%2F%2Fexample.com",
				"/next/console/a%3Fb", "/next/../keys", "/console/next", "/console/next/keys",
				"/api/missing", "/api/user/epay/missing", "/api/stripe/missing", "/api/oauth/missing/callback",
				"/api/../models", "/v1/missing", "/v1beta/missing", "/mj/missing", "/fast/mj/missing",
				"/pg/missing", "/suno/missing", "/kling/v1/missing", "/jimeng/missing",
				"/dashboard/billing/missing", "/dashboard/billing", "/next/api/missing",
				"/playground", "/playground/", "/chat/", "/chat/123", "/chat2link/", "/chat2link/123", "/chat-presets", "/chat-presets/",
				"/console/chat-presets", "/next/chat/123", "/next/console/playground",
				"/system-settings/content/chat-presets", "/next/console/system-settings/content/chat-presets",
				"/models/deployments", "/models/deployments/1", "/deployment", "/deployment/", "/admin/deployments",
				"/console/models/deployments", "/console/deployment", "/console/admin/deployments/1",
				"/next/models/deployments", "/next/deployment", "/next/admin/deployments",
				"/next/console/models/deployments", "/next/console/deployment", "/next/console/admin/deployments",
				"/api/deployments", "/api/deployments/", "/api/deployments/settings", "/api/deployments/settings/test-connection",
				"/api/deployments/123", "/api/deployments/123/logs", "/api/deployments/123/containers",
				"/.vite/manifest.json", "/next/.vite/manifest.json", "/assets/.hidden.js",
			} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
					t.Run(method+path, func(t *testing.T) {
						recorder := serveWebRequest(engine, method, path)
						require.Equal(t, http.StatusNotFound, recorder.Code)
						assert.Empty(t, recorder.Header().Get("Location"))
						assert.NotContains(t, recorder.Body.String(), "vue-index")
						assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
					})
				}
			}
			for _, path := range []string{"/", "/console/keys", "/next", "/next/console/keys", "/assets/app.js"} {
				recorder := serveWebRequest(engine, http.MethodPost, path)
				assert.Equal(t, http.StatusNotFound, recorder.Code, path)
				assert.Empty(t, recorder.Header().Get("Location"), path)
			}
		})
	}
}

func TestVueStaticAssets(t *testing.T) {
	engine := newWebTestRouter(t, testWebAssets("vue-index"), "", true)
	for _, test := range []struct{ path, body, cache string }{
		{"/assets/app-AbCd1234.js", "hashed-asset", "public, max-age=31536000, immutable"},
		{"/assets/_plugin-vue_export-helper-BDNMzG2s.js", "helper", "public, max-age=31536000, immutable"},
		{"/assets/app.js", "asset", "no-cache"},
		{"/logo.png", "logo", "no-cache"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+test.path, func(t *testing.T) {
				recorder := serveWebRequest(engine, method, test.path)
				require.Equal(t, http.StatusOK, recorder.Code)
				if method == http.MethodHead {
					assert.Empty(t, recorder.Body.String())
				} else {
					assert.Equal(t, test.body, recorder.Body.String())
				}
				assert.Equal(t, test.cache, recorder.Header().Get("Cache-Control"))
			})
		}
	}
	for _, path := range []string{"/assets", "/assets/", "/assets/missing.js", "/missing.png"} {
		recorder := serveWebRequest(engine, http.MethodGet, path)
		assert.Equal(t, http.StatusNotFound, recorder.Code, path)
		assert.NotContains(t, recorder.Body.String(), "vue-index", path)
	}
}

func TestVueExternalFrontend(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "master"}[master], func(t *testing.T) {
			engine := newWebTestRouter(t, testWebAssets("vue-index"), "https://frontend.example/", master)
			for _, path := range []string{"/", "/console/keys?return=a%2Fb", "/assets/app.js"} {
				recorder := serveWebRequest(engine, http.MethodGet, path)
				if master {
					assert.Equal(t, http.StatusOK, recorder.Code, path)
					assert.Empty(t, recorder.Header().Get("Location"), path)
				} else {
					assert.Equal(t, http.StatusTemporaryRedirect, recorder.Code, path)
					assert.Equal(t, "https://frontend.example"+path, recorder.Header().Get("Location"), path)
				}
			}
		})
	}
}

func TestVueRegisteredBackendRoutesKeepAuthentication(t *testing.T) {
	engine := newWebTestRouter(t, testWebAssets("vue-index"), "https://frontend.example", false)
	for _, path := range []string{"/api/user/self", "/api/next/wallet/config", "/v1/models", "/dashboard/billing/usage", "/v1/videos/test/content"} {
		recorder := serveWebRequest(engine, http.MethodGet, path)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, path)
		assert.Empty(t, recorder.Header().Get("Location"), path)
		assert.NotContains(t, recorder.Body.String(), "vue-index", path)
	}
}

func TestVuePlaceholderIsUnavailable(t *testing.T) {
	index := `<meta name="ren2hub-frontend-build" content="placeholder">`
	engine := newWebTestRouter(t, testWebAssets(index), "", true)
	for _, path := range []string{"/", "/console/keys"} {
		recorder := serveWebRequest(engine, http.MethodGet, path)
		assert.Equal(t, http.StatusServiceUnavailable, recorder.Code, path)
	}
}
