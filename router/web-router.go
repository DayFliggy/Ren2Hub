package router

import (
	"bytes"
	"io/fs"
	"net/http"
	pathpkg "path"
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
	NextBuildFS   fs.FS
	NextIndexPage []byte
}

const nextPlaceholderMarker = `name="ren2hub-next-build" content="placeholder"`

func nextBuildReady(indexPage []byte) bool {
	return len(indexPage) > 0 && !bytes.Contains(indexPage, []byte(nextPlaceholderMarker))
}

func isWebStaticRequest(requestPath string) bool {
	if strings.HasPrefix(requestPath, "/next/assets/") || strings.HasPrefix(requestPath, "/assets/") {
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
	path := strings.TrimPrefix(requestPath, "/next/")
	if path != requestPath {
		path = "/" + path
	}
	path = strings.TrimPrefix(path, "/console/")
	if path != requestPath && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for _, prefix := range []string{"/playground", "/chat", "/chat2link", "/chat-presets", "/system-settings/content/chat-presets"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isBackendWebRequest(requestPath string) bool {
	for _, prefix := range []string{"/api", "/v1", "/v1beta", "/mj", "/pg", "/suno", "/kling", "/jimeng"} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	return len(parts) > 1 && parts[1] == "mj"
}

func SetWebRouter(router *gin.Engine, assets WebAssets) {
	nextFS := common.EmbedFolder(assets.NextBuildFS, "frontend/embed-dist")
	nextStatic := static.Serve("/next", nextFS)
	publicStatic := static.Serve("/", nextFS)
	nextReady := nextBuildReady(assets.NextIndexPage)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.Cache())
	router.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		// HTML must use the injected index below; only static files bypass rate limiting.
		if !isWebStaticRequest(path) || isBackendWebRequest(path) || isRetiredWebRequest(path) {
			c.Next()
			return
		}
		if strings.HasPrefix(path, "/next/") {
			if strings.HasPrefix(path, "/next/assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			nextStatic(c)
			return
		}
		publicStatic(c)
	})
	router.Use(middleware.GlobalWebRateLimit())
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		c.Header("Cache-Control", "no-cache")
		path := c.Request.URL.Path
		if isBackendWebRequest(path) || isWebStaticRequest(path) || isRetiredWebRequest(path) ||
			(c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			controller.RelayNotFound(c)
			return
		}
		if path == "/next" || strings.HasPrefix(path, "/next/") {
			if !nextReady {
				c.String(http.StatusServiceUnavailable, "next frontend build is unavailable")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.NextIndexPage)
			return
		}
		target := "/next" + c.Request.URL.RequestURI()
		c.Redirect(http.StatusTemporaryRedirect, target)
	})
}
