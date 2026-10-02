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

func TestPluginPricingSimulationUsesExplicitCompatibilityFacts(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previous, secret := jsplugin.DefaultRegistry, common.CryptoSecret
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	common.CryptoSecret = "compatibility-simulation-fixture"
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous; common.CryptoSecret = secret })
	const oldSource = `export const meta={apiVersion:1,key:"compat-simulation",name:"Compatibility",version:"1.0.0",author:{name:"Test"},models:["image"],fetchMode:"per_task",usageProfiles:[{models:["image"],schema:{images:{type:"number",unit:"count"},old_images:{type:"number",unit:"count"}}}]};
export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};`
	_, err := jsplugin.DefaultRegistry.Register(oldSource, jsplugin.Options{})
	require.NoError(t, err)
	pricing := model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("old_images") * 0.01 + u("images") * 0.02`}
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: "image", ExpectedVersion: model.ModelPricingVersion(model.PricingValues{}), Pricing: pricing}}))
	source := strings.Replace(oldSource, `version:"1.0.0"`, `version:"1.1.0"`, 1)
	source = strings.Replace(source, `schema:{images:{type:"number",unit:"count"},old_images:{type:"number",unit:"count"}}`, `schema:{images:{type:"number",unit:"count"}},compatibilitySchema:{old_images:{type:"number",unit:"count"}}`, 1)
	_, err = jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	router := gin.New()
	router.POST("/simulate", SimulateModelPricing)
	for _, test := range []struct {
		name, usage string
		status      int
	}{
		{"legacy fact is available", `{"images":1,"old_images":2}`, http.StatusOK},
		{"missing legacy fact is not invented", `{"images":1}`, http.StatusBadRequest},
		{"legacy fact remains bounded", `{"images":1,"old_images":129}`, http.StatusBadRequest},
		{"undeclared fact is rejected", `{"images":1,"old_images":2,"extra":1}`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"model_name":"image","pricing":{"billing_setting.billing_mode":"tiered_expr","billing_setting.billing_expr":"u(\"old_images\") * 0.01 + u(\"images\") * 0.02"},"usage":%s}`, test.usage)
			request := httptest.NewRequest(http.MethodPost, "/simulate", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code, response.Body.String())
			if test.status == http.StatusOK {
				assert.Contains(t, response.Body.String(), `"amount_usd":"0.04"`)
			}
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.BillingPriceVersion{}).Count(&count).Error)
	assert.Zero(t, count, "preview must not create a frozen runtime price")
}
