package middleware

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// brotliFrontendExtensions are the text build files that
// scripts/prepare-frontend-embed.mjs gives a brotli sibling <file>.br.
var brotliFrontendExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".svg": true, ".json": true, ".webmanifest": true, ".txt": true,
}

// ServeFrontendBrotliFiles sends the brotli sibling written at build time when
// the request accepts br, so the server never compresses these files. It runs
// just before ServeFrontendFiles, which answers every request it passes on.
// A request for several ranges is passed on: ServeContent would answer it
// with a multipart/byteranges body, which is not a brotli stream.
func ServeFrontendBrotliFiles(frontendFS static.ServeFileSystem) gin.HandlerFunc {
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		if !brotliFrontendExtensions[path.Ext(urlPath)] ||
			!acceptsBrotli(c.GetHeader("Accept-Encoding")) ||
			strings.Contains(c.GetHeader("Range"), ",") ||
			!frontendFS.Exists("/", urlPath) {
			return
		}
		file, err := frontendFS.Open(urlPath + ".br")
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		if err == nil {
			defer file.Close()
			err = sendFrontendBrotliFile(c, frontendFS, file, urlPath)
		}
		if err != nil {
			// ServeFrontendFiles answers instead, as when its gzip encoding fails.
			common.SysError(common.LogText("brotli frontend file %s: %v", urlPath, err))
		}
	}
}

func sendFrontendBrotliFile(c *gin.Context, frontendFS http.FileSystem, file http.File, urlPath string) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	contentType, err := frontendContentType(frontendFS, urlPath)
	if err != nil {
		return err
	}
	c.Abort()
	header := c.Writer.Header()
	header.Set("Vary", "Accept-Encoding")
	header.Set("Content-Type", contentType)
	// The .br file is the selected representation: ServeContent answers
	// preconditions, HEAD and ranges for its bytes.
	http.ServeContent(brotliBodyWriter{c.Writer}, c.Request, urlPath, info.ModTime(), file)
	return nil
}

// brotliBodyWriter marks only responses that carry the .br bytes as brotli
// encoded. ServeContent omits Content-Length once Content-Encoding is set, and
// its 304, 412 and 416 answers carry no brotli body. ServeContent always calls
// WriteHeader before writing.
type brotliBodyWriter struct{ http.ResponseWriter }

func (w brotliBodyWriter) WriteHeader(code int) {
	if code == http.StatusOK || code == http.StatusPartialContent {
		w.Header().Set("Content-Encoding", "br")
	}
	w.ResponseWriter.WriteHeader(code)
}

// frontendContentType is the type ServeFrontendFiles and http.FileServer send
// for the original file: by extension, else by sniffing its first 512 bytes.
func frontendContentType(frontendFS http.FileSystem, urlPath string) (string, error) {
	if contentType := mime.TypeByExtension(path.Ext(urlPath)); contentType != "" {
		return contentType, nil
	}
	file, err := frontendFS.Open(urlPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	return http.DetectContentType(head[:n]), nil
}

// acceptsBrotli reports whether an Accept-Encoding value lists br with a
// quality above zero. A missing q means 1; a q that is not an HTTP qvalue
// means no.
func acceptsBrotli(acceptEncoding string) bool {
	for item := range strings.SplitSeq(acceptEncoding, ",") {
		coding, params, _ := strings.Cut(item, ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "br") {
			continue
		}
		quality := 1.0
		for param := range strings.SplitSeq(params, ";") {
			name, value, found := strings.Cut(param, "=")
			if !found || !strings.EqualFold(strings.TrimSpace(name), "q") {
				continue
			}
			parsed, ok := parseQValue(strings.TrimSpace(value))
			if !ok {
				return false
			}
			quality = parsed
		}
		return quality > 0
	}
	return false
}

// parseQValue reads an RFC 9110 qvalue: 0 or 1, optionally followed by a
// point and at most three digits, and not above 1.
func parseQValue(value string) (float64, bool) {
	if len(value) == 0 || len(value) > 5 || (value[0] != '0' && value[0] != '1') {
		return 0, false
	}
	if len(value) > 1 && value[1] != '.' {
		return 0, false
	}
	for _, digit := range value[min(len(value), 2):] {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	quality, err := strconv.ParseFloat(value, 64)
	if err != nil || quality > 1 {
		return 0, false
	}
	return quality, true
}
