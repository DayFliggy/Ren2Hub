package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerRedactsEpayCallbackQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousWriter := gin.DefaultWriter
	var output bytes.Buffer
	gin.DefaultWriter = &output
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })
	engine := gin.New()
	SetUpLogger(engine)
	engine.Any("/api/user/epay/notify", func(c *gin.Context) { c.String(http.StatusOK, "success") })
	engine.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		output.Reset()
		request := httptest.NewRequest(method, "/api/user/epay/notify?sign=secret-signature&pid=private-merchant&out_trade_no=private-order&money=12.34", strings.NewReader("sign=body-signature"))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, output.String(), method+" /api/user/epay/notify\n")
		for _, private := range []string{"secret-signature", "private-merchant", "private-order", "12.34", "body-signature", "?", "sign="} {
			assert.NotContains(t, output.String(), private)
		}
	}
	output.Reset()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health?check=ready", nil))
	assert.Contains(t, output.String(), "/health?check=ready")
}

func TestLoggerRedactsTaskCapabilitiesBeforeCaptureAndPreservesAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousWriter, previousSecret := gin.DefaultWriter, common.CryptoSecret
	var output bytes.Buffer
	gin.DefaultWriter, common.CryptoSecret = &output, "local-artifact-logger-secret"
	t.Cleanup(func() { gin.DefaultWriter, common.CryptoSecret = previousWriter, previousSecret })
	engine := gin.New()
	SetUpLogger(engine)
	engine.GET("/v1/tasks/:key/artifacts/:artifact_key/content", TokenOrTaskArtifactAccessAuth("key", "artifact_key"), func(c *gin.Context) {
		assert.True(t, IsTaskArtifactAccess(c))
		assert.Equal(t, "ready", c.Query("check"))
		assert.NotContains(t, c.Request.RequestURI, "access")
		c.Status(http.StatusNoContent)
	})
	access, err := service.IssueTaskArtifactAccess("task-log", "output")
	require.NoError(t, err)
	for _, test := range []struct {
		query  string
		status int
	}{
		{"?check=ready&access=" + access, http.StatusNoContent},
		{"?access=" + access + "&access=duplicate-secret&check=ready", http.StatusNotFound},
		{"?%61ccess=malformed-secret&check=ready", http.StatusNotFound},
	} {
		output.Reset()
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/tasks/task-log/artifacts/output/content"+test.query, nil))
		assert.Equal(t, test.status, response.Code)
		assert.Contains(t, output.String(), "?check=ready")
		for _, secret := range []string{access, "duplicate-secret", "malformed-secret", "access=", "%61ccess"} {
			assert.NotContains(t, output.String(), secret)
		}
	}
}
