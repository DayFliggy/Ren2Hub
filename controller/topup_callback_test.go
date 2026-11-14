package controller

import (
	"bytes"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"testing"
)

// useStripeWebhookForTest 启用 Stripe 回调，返回一个按签名投递事件的函数。
func useStripeWebhookForTest(t *testing.T) func(payload string) int {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	oldAPI, oldWebhook, oldPrice := setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = oldAPI, oldWebhook, oldPrice
	})
	setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = "sk_test_local", "whsec_local_test", "price_local"
	router := gin.New()
	router.POST("/stripe", StripeWebhook)
	return func(payload string) int {
		signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: []byte(payload), Secret: setting.StripeWebhookSecret})
		request := httptest.NewRequest(http.MethodPost, "/stripe", bytes.NewReader(signed.Payload))
		request.Header.Set("Stripe-Signature", signed.Header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
}

func stripeCheckoutEvent(object string) string {
	return `{"id":"evt_local","type":"checkout.session.completed","data":{"object":{"object":"checkout.session","status":"complete","payment_status":"paid",` + object + `}}}`
}

func countCallbackRejections(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "payment.callback_rejected").Count(&count).Error)
	return count
}

func TestStripeWebhookCreditsFrozenQuotaAcrossSettlementCurrencies(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	deliver := useStripeWebhookForTest(t)
	user := model.User{Username: "stripe-match", AffCode: "STRIPE-MATCH"}
	require.NoError(t, db.Create(&user).Error)
	large := model.TopUp{UserId: user.Id, TradeNo: "stripe-large-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 700, Amount: 10000, Money: 10000, Currency: "USD", PayAmount: "10000"}
	require.NoError(t, large.Insert())
	adaptive := model.TopUp{UserId: user.Id, TradeNo: "stripe-adaptive-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 300, Amount: 5, Money: 5, Currency: "USD", PayAmount: "5"}
	require.NoError(t, adaptive.Insert())

	// 原生回调不核对收据金额或币种；大额和 Adaptive Pricing 的返回字段
	// 均不能改变订单已冻结的到账额度及记录币种。
	assert.Equal(t, http.StatusOK, deliver(stripeCheckoutEvent(`"mode":"payment","client_reference_id":"stripe-large-order","customer":null,"amount_subtotal":1000000,"currency":"usd"`)))
	assert.Equal(t, http.StatusOK, deliver(stripeCheckoutEvent(`"mode":"payment","client_reference_id":"stripe-adaptive-order","amount_subtotal":460,"currency":"eur","currency_conversion":{"amount_subtotal":500,"amount_total":500,"fx_rate":"0.92","source_currency":"usd"}`)))
	assert.Zero(t, countCallbackRejections(t, db))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
	for _, tradeNo := range []string{large.TradeNo, adaptive.TradeNo} {
		saved := model.GetTopUpByTradeNo(tradeNo)
		assert.Equal(t, common.TopUpStatusSuccess, saved.Status, tradeNo)
		assert.Equal(t, "USD", saved.Currency, tradeNo)
	}
}
