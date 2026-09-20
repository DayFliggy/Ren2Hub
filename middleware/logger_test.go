package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
