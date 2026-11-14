//go:build known_upstream_restored

package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRestoredNativeEpayNotifyLogsExcludeCredentials(t *testing.T) {
	restoredCompliance(t)
	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	oldMethods := operation_setting.PayMethods
	common.LogWriterMu.Lock()
	oldWriter, oldErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	var output bytes.Buffer
	gin.DefaultWriter, gin.DefaultErrorWriter = &output, &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
		operation_setting.PayMethods = oldMethods
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = oldWriter, oldErrorWriter
		common.LogWriterMu.Unlock()
	})
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://pay.example.test", "test-merchant", "merchant-secret"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}

	router := gin.New()
	router.Any("/api/user/epay/notify", EpayNotify)
	params := url.Values{
		"pid": {"test-merchant"}, "sign": {"signature-secret"}, "api_key": {"query-secret"}, "token": {"body-secret"},
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequest(method, "/api/user/epay/notify?"+params.Encode(), nil)
		if method == http.MethodPost {
			request = httptest.NewRequest(method, "/api/user/epay/notify", strings.NewReader(params.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		assert.Equal(t, "fail", recorder.Body.String())
	}

	logged := output.String()
	for _, secret := range []string{"signature-secret", "query-secret", "body-secret", "merchant-secret"} {
		assert.False(t, strings.Contains(logged, secret), "credential marker must stay out of logs: %s", secret)
	}
	assert.Contains(t, logged, "Epay webhook received request")
	assert.Contains(t, logged, "signature verification failed")
	assert.Contains(t, logged, "method=GET")
	assert.Contains(t, logged, "method=POST")
}
