package middleware

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// RecordApplicationTraffic counts consumed body bytes, not Content-Length or
// attempted writes. It retains neither payloads nor outcome/billing state.
func RecordApplicationTraffic() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = &trafficReadCloser{ReadCloser: c.Request.Body}
		}
		c.Writer = &trafficResponseWriter{ResponseWriter: c.Writer}
		c.Next()
	}
}

type trafficReadCloser struct{ io.ReadCloser }

func (r *trafficReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	common.ObserveApplicationRequestBytes(n)
	return n, err
}

// Embedding gin.ResponseWriter keeps Flush, Hijack, CloseNotify, Pusher and
// status/header behavior intact. Hijacked frames intentionally bypass counters.
type trafficResponseWriter struct{ gin.ResponseWriter }

func (w *trafficResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *trafficResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	common.ObserveApplicationResponseBytes(n)
	return n, err
}

func (w *trafficResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	common.ObserveApplicationResponseBytes(n)
	return n, err
}
