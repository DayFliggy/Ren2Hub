package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingPriceAPIRevalidatesCurrentPluginWithoutRewritingHistory(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.BillingActivePrice{}))
	previous := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
	source := `export const meta={apiVersion:1,key:"schema-fixture",name:"Schema",version:"2.0.0",author:{name:"Test"},models:["schema-fixture"],fetchMode:"per_task",usageSchema:{resolution:{enum:["720p"]}}};export function buildSubmitRequest(){return {url:"https://example.com"}};export function parseSubmitResponse(){return {taskId:"job"}};export function buildQueryRequest(){return {url:"https://example.com"}};export function parseTaskResult(){return {status:"SUCCESS"}};`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	price := billing.PriceVersion{Model: "schema-fixture", Mode: billing.ModeExpression, Expression: `v2:u("resolution") == "1080p" ? 0.4 : 0.2`, UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{"resolution": {Type: "string", Source: billingexpr.UsageRequestFixed, Required: true, Enum: []string{"720p", "1080p"}}}}}
	old, err := model.PublishBillingPrice(context.Background(), price, 0, 1)
	require.NoError(t, err)
	body, err := common.Marshal(gin.H{"price": old, "expected_version": old.ID, "plugin_key": "schema-fixture"})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/billing/prices", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	PublishBillingPrice(c)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "1080p")
	var count int64
	require.NoError(t, db.Model(&model.BillingPriceVersion{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/billing/prices", nil)
	GetBillingPrices(c)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "1080p", "historical published enum remains visible")
	for _, name := range []string{"ordinary-model", "schema-fixture"} {
		w = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/billing/prices/task-schema?model=%s", name), nil)
		GetTaskPluginBillingSchema(c)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"usage_schema":null`)
		if name == "schema-fixture" {
			assert.Contains(t, w.Body.String(), `"review_required":true`)
			assert.Contains(t, w.Body.String(), `"enum":["720p"]`)
		} else {
			assert.Contains(t, w.Body.String(), `"review_required":false`)
			assert.Contains(t, w.Body.String(), `"plugins":[]`)
		}
	}
}
