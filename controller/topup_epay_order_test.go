package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both order endpoints must enforce database-backed configuration and the same
// global and per-method minimum before persisting a payment order.
func TestCreateEpayTopUpOrderValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, optionKey, optionValue string
		amount                       int64
		method, wantCode             string
	}{
		{"unavailable", "payment_setting.compliance_confirmed", "false", 10, "alipay", "PAYMENT_UNAVAILABLE"},
		{"unsupported method", "", "", 10, "paypal", "PAYMENT_UNAVAILABLE"},
		{"empty method", "", "", 10, "", "PAYMENT_UNAVAILABLE"},
		{"global minimum", "MinTopUp", "5", 4, "alipay", "VALIDATION_ERROR"},
		{"method minimum", "PayMethods", `[{"name":"支付宝","type":"alipay","min_topup":"50"}]`, 10, "alipay", "VALIDATION_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupEpayControllerTestDB(t)
			if test.optionKey != "" {
				require.NoError(t, db.Save(&model.Option{Key: test.optionKey, Value: test.optionValue}).Error)
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			order, orderErr := createEpayTopUpOrder(ctx, 1, test.amount, test.method)
			require.NotNil(t, orderErr)
			assert.Nil(t, order)
			assert.Equal(t, test.wantCode, orderErr.Code)
			var count int64
			require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

// A quote rejection keeps its stable code and reaches the web console as a
// message key with display-ready params, on the Vue contract and on the legacy
// payment endpoints, whose {"message":"error","data":text} shape is unchanged.
func TestEpayRejectionCarriesMessageKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEpayControllerTestDB(t)
	require.NoError(t, db.Save(&model.Option{Key: "MinTopUp", Value: "5"}).Error)
	const text = "Top-up quota cannot be less than 5 USD"
	const key = `"message_key":"Top-up quota cannot be less than {{min}} USD","message_params":{"min":5}`
	for _, test := range []struct {
		name    string
		handler gin.HandlerFunc
		want    string
	}{
		{"vue quote", NextQuoteEpayTopUp, `{"success":false,"code":"VALIDATION_ERROR","message":"` + text + `",` + key + `}`},
		{"legacy order", RequestEpay, `{"message":"error","data":"` + text + `",` + key + `}`},
		{"legacy amount", RequestAmount, `{"message":"error","data":"` + text + `",` + key + `}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("id", 1)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"amount":4,"amount_unit":"USD","payment_method":"alipay"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			test.handler(ctx)
			assert.JSONEq(t, test.want, recorder.Body.String())
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
}
