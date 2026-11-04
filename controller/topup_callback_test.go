package controller

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81/webhook"
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/core"
	"github.com/waffo-com/waffo-go/utils"
	"gorm.io/gorm"
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

func TestStripeWebhookAcknowledgesPermanentRejections(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	deliver := useStripeWebhookForTest(t)
	user := model.User{Username: "stripe-reject", AffCode: "STRIPE-REJECT"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "stripe-reject-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 1000, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
	require.NoError(t, order.Insert())
	foreign := model.TopUp{UserId: user.Id, TradeNo: "stripe-foreign-order", PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, ExpectedQuota: 1000, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
	require.NoError(t, foreign.Insert())

	// 这些回调重试也不会成功：确认收到（200）、写审计、不入账。
	for name, payload := range map[string]string{
		"unknown order":    stripeCheckoutEvent(`"mode":"payment","client_reference_id":"missing-order","amount_subtotal":100,"currency":"usd"`),
		"amount mismatch":  stripeCheckoutEvent(`"mode":"payment","client_reference_id":"stripe-reject-order","amount_subtotal":99,"currency":"usd"`),
		"currency":         stripeCheckoutEvent(`"mode":"payment","client_reference_id":"stripe-reject-order","amount_subtotal":100,"currency":"eur"`),
		"foreign provider": stripeCheckoutEvent(`"mode":"payment","client_reference_id":"stripe-foreign-order","amount_subtotal":100,"currency":"usd"`),
		"subscription":     stripeCheckoutEvent(`"mode":"subscription","client_reference_id":"stripe-reject-order","amount_subtotal":100,"currency":"usd"`),
	} {
		assert.Equal(t, http.StatusOK, deliver(payload), name)
	}
	assert.EqualValues(t, 5, countCallbackRejections(t, db))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(foreign.TradeNo).Status)
}

func TestStripeWebhookMatchesSourceCurrencyAndLargeAmounts(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	deliver := useStripeWebhookForTest(t)
	user := model.User{Username: "stripe-match", AffCode: "STRIPE-MATCH"}
	require.NoError(t, db.Create(&user).Error)
	large := model.TopUp{UserId: user.Id, TradeNo: "stripe-large-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 700, Amount: 10000, Money: 10000, Currency: "USD", PayAmount: "10000"}
	require.NoError(t, large.Insert())
	adaptive := model.TopUp{UserId: user.Id, TradeNo: "stripe-adaptive-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 300, Amount: 5, Money: 5, Currency: "USD", PayAmount: "5"}
	require.NoError(t, adaptive.Insert())

	// 1000000 分曾被格式化成 1e+06；Adaptive Pricing 按创建时的源币种核对，而不是买家本地币种。
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

func TestCreemWebhookRetriesOnlyTransientFailures(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	confirmPaymentComplianceForTest(t)
	oldKey, oldProducts, oldSecret := setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = oldKey, oldProducts, oldSecret
	})
	setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = "creem_api_key", `[{"productId":"prod_local"}]`, "creem_secret"
	router := gin.New()
	router.POST("/creem", CreemWebhook)
	deliver := func(requestID, orderType string) int {
		body := `{"id":"evt_local","eventType":"checkout.completed","object":{"request_id":"` + requestID + `","order":{"id":"ord_local","status":"paid","type":"` + orderType + `","amount_paid":100,"currency":"USD"},"customer":{"email":"buyer@example.com","name":"Buyer"}}}`
		request := httptest.NewRequest(http.MethodPost, "/creem", strings.NewReader(body))
		request.Header.Set(CreemSignatureHeader, generateCreemSignature(body, setting.CreemWebhookSecret))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	user := model.User{Username: "creem-retry", AffCode: "CREEM-RETRY"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "creem-retry-order", PaymentProvider: model.PaymentProviderCreem, PaymentMethod: model.PaymentMethodCreem, Status: common.TopUpStatusPending, ExpectedQuota: 1000, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
	require.NoError(t, order.Insert())

	assert.Equal(t, http.StatusOK, deliver(order.TradeNo, "recurring"), "retired subscription callbacks are acknowledged")
	assert.Equal(t, http.StatusOK, deliver("missing-order", "onetime"), "an unknown order never succeeds on retry")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail_creem_wallet", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("test wallet unavailable"))
		}
	}))
	assert.Equal(t, http.StatusInternalServerError, deliver(order.TradeNo, "onetime"), "a transient failure asks Creem to retry")
	require.NoError(t, db.Callback().Update().Remove("fail_creem_wallet"))
	assert.Equal(t, http.StatusOK, deliver(order.TradeNo, "onetime"))
	assert.Equal(t, http.StatusOK, deliver(order.TradeNo, "onetime"))
	assert.EqualValues(t, 2, countCallbackRejections(t, db), "transient failures are not audited as rejections")
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
	assert.Equal(t, "buyer@example.com", user.Email)
}

func TestUpstreamSyncStripeRetriesTransientWalletFailure(t *testing.T) {
	db := useGmpayControllerDB(t)
	deliver := useStripeWebhookForTest(t)
	user := model.User{Username: "stripe-outage", AffCode: "stripe-outage", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "stripe-outage", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1", ExpectedQuota: 1000}
	require.NoError(t, order.Insert())
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("upstream-sync:stripe-outage", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("temporary wallet failure"))
		}
	}))
	payload := stripeCheckoutEvent(`"mode":"payment","client_reference_id":"stripe-outage","amount_subtotal":100,"currency":"usd"`)
	assert.Equal(t, http.StatusInternalServerError, deliver(payload), "ACK would lose a successful payment when the wallet temporarily fails")
	require.NoError(t, db.Callback().Update().Remove("upstream-sync:stripe-outage"))
	assert.Equal(t, http.StatusOK, deliver(payload))
	assert.Equal(t, http.StatusOK, deliver(payload))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
}
func TestUpstreamSyncWaffoAcknowledgesOnlyPermanentFailures(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "waffo-retry", AffCode: "waffo-retry", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "waffo-expired", PaymentProvider: model.PaymentProviderWaffo, PaymentMethod: model.PaymentMethodWaffo, Status: common.TopUpStatusExpired, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1", ExpectedQuota: 1000}
	require.NoError(t, db.Create(&order).Error)
	pair, err := utils.GenerateKeyPair()
	require.NoError(t, err)
	handler := core.NewWebhookHandler(&config.WaffoConfig{APIKey: "local-fixture", PrivateKey: pair.PrivateKey, WaffoPublicKey: pair.PublicKey})
	deliver := func(id string) string {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/waffo", nil)
		// handleWaffoPayment is called only after WaffoWebhook verified the payload.
		handleWaffoPayment(c, handler, &core.PaymentNotificationResult{MerchantOrderID: id, OrderStatus: "PAY_SUCCESS"})
		require.True(t, utils.Verify(response.Body.String(), response.Header().Get("X-SIGNATURE"), pair.PublicKey))
		return response.Body.String()
	}
	assert.JSONEq(t, `{"message":"success"}`, deliver(order.TradeNo), "expired orders can never become payable on retry")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("upstream-sync:waffo-outage", func(tx *gorm.DB) {
		if tx.Statement.Table == "top_ups" {
			tx.AddError(errors.New("temporary lookup failure"))
		}
	}))
	assert.JSONEq(t, `{"message":"failed"}`, deliver(order.TradeNo), "database outages remain retryable")
	require.NoError(t, db.Callback().Query().Remove("upstream-sync:waffo-outage"))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
}
func TestUpstreamSyncPancakeAcknowledgesPermanentVerifiedFailure(t *testing.T) {
	db := useGmpayControllerDB(t)
	confirmPaymentComplianceForTest(t)
	oldMerchant, oldKey, oldProduct, oldStore := setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID, setting.WaffoPancakeStoreID
	t.Cleanup(func() {
		setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID, setting.WaffoPancakeStoreID = oldMerchant, oldKey, oldProduct, oldStore
	})
	setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID, setting.WaffoPancakeStoreID = "local-merchant", "presence-only", "local-product", "local-store"
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	require.NoError(t, err)
	t.Setenv("WAFFO_WEBHOOK_TEST_PUBLIC_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})))
	user := model.User{Username: "pancake-expired", AffCode: "pancake-expired", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "pancake-expired", PaymentProvider: model.PaymentProviderWaffoPancake, PaymentMethod: model.PaymentMethodWaffoPancake, Status: common.TopUpStatusExpired, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1", ExpectedQuota: 1000}
	require.NoError(t, db.Create(&order).Error)
	body, err := json.Marshal(map[string]any{"id": "evt_local", "timestamp": time.Now().UTC().Format(time.RFC3339), "eventType": "order.completed", "eventId": "event_local", "storeId": "local-store", "mode": "test", "data": map[string]any{"orderId": "order_local", "orderMerchantExternalId": order.TradeNo, "merchantProvidedBuyerIdentity": service.WaffoPancakeBuyerIdentityFromUserID(user.Id), "currency": "USD", "amount": "1"}})
	require.NoError(t, err)
	timestamp := fmt.Sprint(time.Now().UnixMilli())
	digest := sha256.Sum256([]byte(timestamp + "." + string(body)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
	require.NoError(t, err)
	header := "t=" + timestamp + ",v1=" + base64.StdEncoding.EncodeToString(signature)
	router := gin.New()
	router.POST("/pancake/:env", WaffoPancakeWebhook)
	request := httptest.NewRequest(http.MethodPost, "/pancake/test", bytes.NewReader(body))
	request.Header.Set("X-Waffo-Signature", header)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusOK, response.Code, "an expired verified order can never succeed on retry")
	assert.Equal(t, "OK", response.Body.String())
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
}
