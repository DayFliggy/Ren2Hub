package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gmpayConfigRow(t *testing.T) string {
	t.Helper()
	var option model.Option
	require.NoError(t, model.DB.Where(&model.Option{Key: setting.GmpayConfigKey}).First(&option).Error)
	return option.Value
}

func TestSaveGmpayConfigNamesValidationFailures(t *testing.T) {
	useGmpayControllerDB(t)
	// The stored config has no secret, so an enabled save with a blank secret
	// cannot inherit one and must fail validation.
	stored := setting.GmpayConfig{PayAddress: "https://gateway.example.test", MerchantID: "merchant", Assets: []setting.GmpayAsset{{Name: "USDT", Token: "usdt", Network: "tron"}}}
	require.NoError(t, service.SaveGmpayConfig(context.Background(), stored))
	before := gmpayConfigRow(t)
	engine := gin.New()
	engine.POST("/save", SaveGmpayConfig)

	asset := `[{"name":"USDT","token":"usdt","network":"tron"}]`
	cases := []struct {
		name, body, message string
	}{
		{"http gateway", `{"pay_address":"http://gateway.example.test","merchant_id":"m","assets":` + asset + `}`, "Invalid GMPay merchant settings"},
		{"gateway query", `{"pay_address":"https://gateway.example.test/?a=1","merchant_id":"m","assets":` + asset + `}`, "Invalid GMPay merchant settings"},
		{"invalid token", `{"pay_address":"https://gateway.example.test","merchant_id":"m","assets":[{"name":"USDT","token":"US DT","network":"tron"}]}`, "Invalid GMPay asset"},
		{"duplicate asset", `{"pay_address":"https://gateway.example.test","merchant_id":"m","assets":[{"name":"A","token":"usdt","network":"tron"},{"name":"B","token":"USDT","network":"TRON"}]}`, "Duplicate GMPay asset"},
		{"enabled without secret", `{"pay_address":"https://gateway.example.test","merchant_id":"m","enabled":true,"assets":` + asset + `}`, "Invalid GMPay merchant settings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/save", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(response, request)
			var body struct {
				Success    bool   `json:"success"`
				Message    string `json:"message"`
				MessageKey string `json:"message_key"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.False(t, body.Success)
			assert.Equal(t, tc.message, body.Message)
			assert.Equal(t, tc.message, body.MessageKey)
			assert.Equal(t, before, gmpayConfigRow(t))
		})
	}
}

func TestGmpaySaveErrorMapsSentinelsAndLogsOnlyUnexpectedErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/save", nil)

	cases := []struct {
		err     error
		message string
	}{
		{gmpay.ErrInvalidConfig, "Invalid GMPay merchant settings"},
		{fmt.Errorf("validate: %w", setting.ErrGmpayInvalidAsset), "Invalid GMPay asset"},
		{fmt.Errorf("validate: %w", setting.ErrGmpayDuplicateAsset), "Duplicate GMPay asset"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.message, gmpaySaveError(ctx, tc.err).Key)
	}
	assert.Empty(t, output.String())

	assert.Equal(t, "Failed to save configuration", gmpaySaveError(ctx, errors.New("database is locked")).Key)
	assert.Contains(t, output.String(), "database is locked")
}
