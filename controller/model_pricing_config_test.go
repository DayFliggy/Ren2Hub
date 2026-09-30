package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingConfigAPISimulationUsesRequestAndTimeWithoutWrites(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	secret := common.CryptoSecret
	common.CryptoSecret = "simulation-test-key"
	t.Cleanup(func() { common.CryptoSecret = secret })
	router := gin.New()
	router.POST("/simulate", SimulateModelPricing)
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"token", `{"model_name":"test","pricing":{"ModelRatio":"1","CompletionRatio":"2"},"usage":{"p":1000,"c":500}}`, "0.004", 200},
		{"request zero", `{"model_name":"test","pricing":{"ModelPrice":"0"},"usage":{}}`, "0", 200},
		{"image count", `{"model_name":"image","pricing":{"ModelPrice":"0.04"},"usage":{"image_count":3}}`, "0.12", 200},
		{"dynamic request and time", `{"model_name":"test","pricing":{"billing_setting.billing_mode":"tiered_expr","billing_setting.billing_expr":"tier(\"base\", p * (param(\"multiplier\") == 2 ? 2 : 1)) * (header(\"X-Test\") == \"yes\" ? 2 : 1) * (hour(\"UTC\") < 12 ? 1 : 3)"},"usage":{"p":1000},"request":{"params":{"multiplier":2,"prompt":"private-input"},"headers":{"X-Test":"yes","Authorization":"private-credential"}},"time":"2026-09-30T13:00:00Z"}`, "0.012", 200},
		{"unpriced", `{"model_name":"test","pricing":{},"usage":{"p":1000}}`, "", 400},
		{"negative usage", `{"model_name":"test","pricing":{"ModelPrice":"1"},"usage":{"p":-1}}`, "", 400},
		{"fractional tokens", `{"model_name":"test","pricing":{"ModelRatio":"1"},"usage":{"p":0.1}}`, "", 400},
		{"missing usage", `{"model_name":"test","pricing":{"ModelPrice":"0"}}`, "", 400},
		{"bad time", `{"model_name":"test","pricing":{"ModelPrice":"0"},"usage":{},"time":"tomorrow"}`, "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/simulate", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			if tc.status == 200 {
				assert.Contains(t, response.Body.String(), `"amount_usd":"`+tc.want+`"`)
			}
			assert.NotContains(t, response.Body.String(), "private-input")
			assert.NotContains(t, response.Body.String(), "private-credential")
			assert.NotContains(t, response.Body.String(), "evaluation_evidence")
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.Option{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&model.BillingPriceVersion{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestModelPricingConfigAPISimulatesTaskProviderSchemaAndRuntimeInputs(t *testing.T) {
	setupModelListControllerTestDB(t)
	previous, secret := jsplugin.DefaultRegistry, common.CryptoSecret
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	common.CryptoSecret = "task-simulation-test-key"
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous; common.CryptoSecret = secret })
	for _, key := range []string{"alpha", "beta"} {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:%q,version:"1.0.0",author:{name:"Test"},models:["task-model"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second"},quality:{enum:["low","high"]},audio:{type:"boolean"}}};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};`, key, key)
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	router := gin.New()
	router.POST("/simulate", SimulateModelPricing)
	for _, tc := range []struct {
		plugin, usage string
		status        int
	}{
		{"alpha", `{"seconds":10,"quality":"high","audio":true}`, 200},
		{"", `{"seconds":10,"quality":"high","audio":true}`, 400},
		{"alpha", `{"seconds":10,"quality":"invalid","audio":true}`, 400},
		{"alpha", `{"seconds":3601,"quality":"high","audio":true}`, 400},
	} {
		body := fmt.Sprintf(`{"model_name":"task-model","plugin_key":%q,"pricing":{"billing_setting.plugin_billing_expr":{"alpha":"u(\"seconds\") * (u(\"quality\") == \"high\" ? 0.4 : 0.2) * (u(\"audio\") ? 2 : 1) * (param(\"multiplier\") == 2 ? 2 : 1) * (header(\"X-Test\") == \"yes\" ? 2 : 1) * (hour(\"UTC\") == 13 ? 1 : 3)"}},"usage":%s,"request":{"params":{"multiplier":2},"headers":{"X-Test":"yes"}},"time":"2026-10-01T13:00:00Z"}`, tc.plugin, tc.usage)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/simulate", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, tc.status, response.Code, response.Body.String())
		if tc.status == 200 {
			assert.Contains(t, response.Body.String(), `"amount_usd":"8"`)
		}
	}
}
