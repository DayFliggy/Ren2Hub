package controller

import (
	"net/http/httptest"
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
