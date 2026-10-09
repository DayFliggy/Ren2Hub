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
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded Vue application.
type WebAssets struct {
	BuildFS   fs.FS
	IndexPage []byte
}

const frontendPlaceholderMarker = `name="ren2hub-frontend-build" content="placeholder"`

var immutableWebAsset = regexp.MustCompile(`^/assets/[^/]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

var retiredWebPathPrefixes = []string{
	"/next", "/playground", "/chat", "/chat2link", "/chat-presets",
	"/subscription", "/plan-management", "/plans",
	"/models/deployments", "/deployment", "/admin/deployments",
	"/system-settings/content/chat", "/system-settings/content/chats", "/system-settings/content/chat-presets",
}

func frontendBuildReady(indexPage []byte) bool {
	return len(indexPage) > 0 && !bytes.Contains(indexPage, []byte(frontendPlaceholderMarker))
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

// frontendFiles narrows the embedded build to the files served as static
// assets. Directories, the index page and paths outside the static extension
// whitelist are not files here: they belong to the SPA fallback and count
// against the web limiter. The upstream static limiter and precompression
// both decide through this one Exists.
type frontendFiles struct {
	static.ServeFileSystem
}

func (files frontendFiles) Exists(_ string, requestPath string) bool {
	requestPath = pathpkg.Clean("/" + strings.TrimLeft(requestPath, "/"))
	if !isWebStaticRequest(requestPath) {
		return false
	}
	file, err := files.Open(requestPath)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && !info.IsDir()
}

func SetWebRouter(router *gin.Engine, assets WebAssets) {
	frontendBaseURL := strings.TrimRight(os.Getenv("FRONTEND_BASE_URL"), "/")
	if common.IsMasterNode && frontendBaseURL != "" {
		frontendBaseURL = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	frontendFS := frontendFiles{common.EmbedFolder(assets.BuildFS, "frontend/embed-dist")}
	frontendReady := frontendBuildReady(assets.IndexPage)
	webRateLimit := middleware.GlobalWebRateLimit(frontendFS)
	serveFrontendFile := middleware.ServeFrontendFiles(frontendFS)
	compress := gzip.Gzip(gzip.DefaultCompression)
	// ServeFrontendFiles sends build files already compressed; pages, missing
	// paths and backend 404s still go through the gzip middleware.
	compressUnlessFrontendFile := func(c *gin.Context) {
		if !frontendFS.Exists("/", c.Request.URL.Path) {
			compress(c)
		}
	}

	serveWeb := func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		c.Header("Cache-Control", "no-cache")
		requestPath := c.Request.URL.Path
		blockedPath := isBackendWebRequest(requestPath) || isRetiredWebRequest(requestPath) || strings.Contains(requestPath, "/.")
		// Keep redirect paths relative to the frontend origin and classify traversal before fallback.
		requestPath = pathpkg.Clean("/" + strings.TrimLeft(requestPath, "/"))
		if blockedPath || isBackendWebRequest(requestPath) || isRetiredWebRequest(requestPath) ||
			strings.Contains(requestPath, "/.") ||
			(c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			c.Header("Cache-Control", "no-store")
			controller.RelayNotFound(c)
			return
		}
		webRateLimit(c)
		if c.IsAborted() {
			return
		}
		if frontendBaseURL != "" {
			target := (&url.URL{Path: requestPath, RawQuery: c.Request.URL.RawQuery}).String()
			c.Redirect(http.StatusTemporaryRedirect, frontendBaseURL+target)
			return
		}
		if isWebStaticRequest(requestPath) {
			if !frontendFS.Exists("/", requestPath) {
				c.Header("Cache-Control", "no-store")
				controller.RelayNotFound(c)
				return
			}
			if immutableWebAsset.MatchString(requestPath) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			serveFrontendFile(c)
			return
		}
		if !frontendReady {
			c.String(http.StatusServiceUnavailable, "frontend build is unavailable")
			return
		}
		if c.Request.Method == http.MethodHead {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusOK)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
	}
	// Exact web roots prevent Gin's relay wildcard from issuing a trailing-slash
	// redirect before the fallback runs; API trailing-slash behavior stays intact.
	for _, path := range []string{"/", "/assets", "/assets/"} {
		router.Any(path, compressUnlessFrontendFile, serveWeb)
	}
	for _, path := range retiredWebPathPrefixes {
		router.Any(path, compressUnlessFrontendFile, serveWeb)
		router.Any(path+"/", compressUnlessFrontendFile, serveWeb)
	}
	router.NoRoute(compressUnlessFrontendFile, serveWeb)
}
