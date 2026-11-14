//go:build known_upstream_restored

package middleware

import (
	"bytes"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredProbePluginMultipartTextBudget(t *testing.T) {
	t.Run("multipart text can exceed file memory threshold", func(t *testing.T) {
		previousBodyLimit := constant.MaxRequestBodyMB
		previousFileLimit := constant.MaxFileDownloadMB
		constant.MaxRequestBodyMB = 16
		constant.MaxFileDownloadMB = 1
		t.Cleanup(func() {
			constant.MaxRequestBodyMB = previousBodyLimit
			constant.MaxFileDownloadMB = previousFileLimit
		})

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		field := strings.Repeat("a", 12<<20)
		require.NoError(t, writer.WriteField("request", field))
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/body", bytes.NewReader(body.Bytes()))
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		request, err := buildTaskPluginRouteRequest(c)
		require.NoError(t, err)
		decoded := request.Body.(map[string]any)
		assert.Len(t, decoded["fields"].(map[string][]string)["request"][0], len(field))
	})

}
