package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingPreviewUsesFrozenEngineWithoutDatabase(t *testing.T) {
	previous := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = previous })
	price := billing.PriceVersion{Model: "video", Mode: billing.ModeExpression, Expression: `v2:u("seconds") * 0.4`, UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"seconds": {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Required: true, Maximum: "60"},
	}}}
	at := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	usage := billing.Usage{Complete: true, ActualFacts: map[string]interface{}{"seconds": "10"}}
	request := map[string]interface{}{"price": price, "usage": usage, "channel_ratio": "1", "frozen_at": at}
	body, err := common.Marshal(request)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/billing/prices/preview", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	PreviewBillingPrice(c)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result struct {
		Success bool
		Data    struct {
			Quote  billing.Quote
			Charge billing.ChargeResult
		}
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result.Success)
	expected, err := billing.Calculate(&result.Data.Quote, usage)
	require.NoError(t, err)
	assert.Equal(t, *expected, result.Data.Charge)
	assert.Equal(t, "4", result.Data.Charge.TotalUSD)
	assert.EqualValues(t, 2000000, result.Data.Charge.FinalQuota)
}
