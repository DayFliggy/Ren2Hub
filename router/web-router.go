package router

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded Vue application.
type WebAssets struct {
	NextBuildFS   fs.FS
	NextIndexPage []byte
}

const nextPlaceholderMarker = `name="ren2hub-next-build" content="placeholder"`

var immutableWebAsset = regexp.MustCompile(`^/assets/[^/]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

var retiredWebPathPrefixes = []string{"/playground", "/chat", "/chat2link", "/chat-presets", "/system-settings/content/chat", "/system-settings/content/chats", "/system-settings/content/chat-presets"}

func nextBuildReady(indexPage []byte) bool {
	return len(indexPage) > 0 && !bytes.Contains(indexPage, []byte(nextPlaceholderMarker))
}

func isWebStaticRequest(requestPath string) bool {
	if requestPath == "/assets" || strings.HasPrefix(requestPath, "/assets/") {
		return true
	}
	// Model identifiers may contain dots; only known asset extensions are files.
	switch strings.ToLower(pathpkg.Ext(requestPath)) {
	case ".js", ".mjs", ".css", ".map", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp", ".avif", ".woff", ".woff2", ".ttf", ".otf", ".mp4", ".webm", ".mp3", ".wav", ".json", ".txt", ".xml", ".webmanifest":
		return true
	}
	return false
}

func isRetiredWebRequest(requestPath string) bool {
	path := requestPath
	if strings.HasPrefix(path, "/console/") {
		path = strings.TrimPrefix(path, "/console")
	}
	for _, prefix := range retiredWebPathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isBackendWebRequest(requestPath string) bool {
	for _, prefix := range []string{"/api", "/v1", "/v1beta", "/mj", "/pg", "/suno", "/kling", "/jimeng", "/dashboard/billing"} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	return len(parts) > 1 && parts[1] == "mj"
}

func SetWebRouter(router *gin.Engine, assets WebAssets) {
	frontendBaseURL := strings.TrimRight(os.Getenv("FRONTEND_BASE_URL"), "/")
	if common.IsMasterNode && frontendBaseURL != "" {
		frontendBaseURL = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	buildFS, err := fs.Sub(assets.NextBuildFS, "frontend/embed-dist")
	if err != nil {
		panic(err)
	}
	publicStatic := http.FileServer(http.FS(buildFS))
	nextReady := nextBuildReady(assets.NextIndexPage)
	webRateLimit := middleware.GlobalWebRateLimit()

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	serveWeb := func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		c.Header("Cache-Control", "no-cache")
		requestPath := c.Request.URL.Path
		legacy := requestPath == "/next" || strings.HasPrefix(requestPath, "/next/")
		if legacy {
			requestPath = strings.TrimPrefix(requestPath, "/next")
		}
		blockedPath := isBackendWebRequest(requestPath) || isRetiredWebRequest(requestPath) || strings.Contains(requestPath, "/.")
		// Keep redirect paths relative to the frontend origin and classify traversal before fallback.
		requestPath = pathpkg.Clean("/" + strings.TrimLeft(requestPath, "/"))
		if blockedPath || isBackendWebRequest(requestPath) || isRetiredWebRequest(requestPath) ||
			strings.Contains(requestPath, "/.") ||
			(c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			controller.RelayNotFound(c)
			return
		}
		staticRequest := isWebStaticRequest(requestPath)
		staticExists := false
		if staticRequest {
			file, err := fs.Stat(buildFS, strings.TrimPrefix(requestPath, "/"))
			staticExists = err == nil && !file.IsDir()
		}
		if !staticExists {
			webRateLimit(c)
			if c.IsAborted() {
				return
			}
		}
		if legacy || frontendBaseURL != "" {
			target := (&url.URL{Path: requestPath, RawQuery: c.Request.URL.RawQuery}).String()
			c.Redirect(http.StatusTemporaryRedirect, frontendBaseURL+target)
			return
		}
		if staticRequest {
			if !staticExists {
				controller.RelayNotFound(c)
				return
			}
			if immutableWebAsset.MatchString(requestPath) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			publicStatic.ServeHTTP(c.Writer, c.Request)
			return
		}
		if !nextReady {
			c.String(http.StatusServiceUnavailable, "frontend build is unavailable")
			return
		}
		if c.Request.Method == http.MethodHead {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusOK)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", assets.NextIndexPage)
	}
	// Exact web roots prevent Gin's relay wildcard from issuing a trailing-slash
	// redirect before the fallback runs; API trailing-slash behavior stays intact.
	for _, path := range []string{"/", "/next", "/next/", "/assets", "/assets/"} {
		router.Any(path, serveWeb)
	}
	for _, path := range retiredWebPathPrefixes {
		router.Any(path, serveWeb)
		router.Any(path+"/", serveWeb)
	}
	router.NoRoute(serveWeb)
}
