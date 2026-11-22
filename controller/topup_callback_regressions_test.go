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
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/core"
	"github.com/waffo-com/waffo-go/utils"
	"gorm.io/gorm"
)

func TestRestoredNativeStripeWebhookAcknowledgesPermanentRejections(t *testing.T) {
	db := restoredFundsDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	deliver := restoredStripeWebhook(t)
	user := model.User{Username: "stripe-reject", AffCode: "STRIPE-REJECT"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "stripe-reject-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
	restoredOrder(t, &order)
	foreign := model.TopUp{UserId: user.Id, TradeNo: "stripe-foreign-order", PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, Amount: 1, Money: 1}
	restoredOrder(t, &foreign)

	// 这些回调重试也不会成功：确认收到（200）、写审计、不入账。
	for name, payload := range map[string]string{
		"unknown order":    restoredStripeEvent(`"mode":"payment","client_reference_id":"missing-order","amount_subtotal":100,"currency":"usd"`),
		"amount mismatch":  restoredStripeEvent(`"mode":"payment","client_reference_id":"stripe-reject-order","amount_subtotal":99,"currency":"usd"`),
		"currency":         restoredStripeEvent(`"mode":"payment","client_reference_id":"stripe-reject-order","amount_subtotal":100,"currency":"eur"`),
		"foreign provider": restoredStripeEvent(`"mode":"payment","client_reference_id":"stripe-foreign-order","amount_subtotal":100,"currency":"usd"`),
		"subscription":     restoredStripeEvent(`"mode":"subscription","client_reference_id":"stripe-reject-order","amount_subtotal":100,"currency":"usd"`),
	} {
		assert.Equal(t, http.StatusOK, deliver(payload), name)
	}
	assert.EqualValues(t, 5, restoredRejections(t, db))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(foreign.TradeNo).Status)
}

func TestRestoredNativeStripeReceiptCasesKeepWalletUnchanged(t *testing.T) {
	for _, scenario := range []struct{ name, fields string }{{"currency", `"amount_subtotal":100,"currency":"eur"`}, {"amount", `"amount_subtotal":99,"currency":"usd"`}, {"missing", `"amount_subtotal":0,"currency":""`}} {
		t.Run(scenario.name, func(t *testing.T) {
			db := restoredFundsDB(t)
			deliver := restoredStripeWebhook(t)
			u := model.User{Username: "receipt-" + scenario.name, AffCode: "receipt-" + scenario.name}
			require.NoError(t, db.Create(&u).Error)
			order := model.TopUp{UserId: u.Id, TradeNo: "receipt-" + scenario.name, PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
			restoredOrder(t, &order)
			assert.Equal(t, http.StatusOK, deliver(restoredStripeEvent(`"mode":"payment","client_reference_id":"`+order.TradeNo+`",`+scenario.fields)))
			require.NoError(t, db.First(&u, u.Id).Error)
			assert.Zero(t, u.Quota, "signature alone cannot authorize an amount or currency different from the created order")
			assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
			assert.EqualValues(t, 1, restoredRejections(t, db), "every rejected callback is audited")
		})
	}
}

func TestRestoredNativeCreemWebhookRetriesOnlyTransientFailures(t *testing.T) {
	db := restoredFundsDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	restoredCompliance(t)
	oldKey, oldProducts, oldSecret := setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = oldKey, oldProducts, oldSecret
	})
	setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = "creem_api_key", `[{"productId":"prod_local"}]`, "creem_secret"
	router := gin.New()
	router.POST("/creem", CreemWebhook)
	deliver := func(requestID, orderType string) int {
		body := `{"id":"evt_local","eventType":"checkout.completed","object":{"status":"completed","units":1,"request_id":"` + requestID + `","order":{"id":"ord_local","status":"paid","type":"` + orderType + `","amount":100,"amount_paid":100,"currency":"USD"},"customer":{"email":"buyer@example.com","name":"Buyer"}}}`
		request := httptest.NewRequest(http.MethodPost, "/creem", strings.NewReader(body))
		request.Header.Set(CreemSignatureHeader, generateCreemSignature(body, setting.CreemWebhookSecret))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	user := model.User{Username: "creem-retry", AffCode: "CREEM-RETRY"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "creem-retry-order", PaymentProvider: model.PaymentProviderCreem, PaymentMethod: model.PaymentMethodCreem, Status: common.TopUpStatusPending, Amount: 1000, Money: 1}
	restoredOrder(t, &order)

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
	assert.EqualValues(t, 2, restoredRejections(t, db), "transient failures are not audited as rejections")
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
	assert.Equal(t, "buyer@example.com", user.Email)
}

func TestRestoredNativeUpstreamSyncWaffoAcknowledgesOnlyPermanentFailures(t *testing.T) {
	db := restoredFundsDB(t)
	user := model.User{Username: "waffo-retry", AffCode: "waffo-retry", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "waffo-expired", PaymentProvider: model.PaymentProviderWaffo, PaymentMethod: model.PaymentMethodWaffo, Status: common.TopUpStatusExpired, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
	require.NoError(t, db.Create(&order).Error)
	pair, err := utils.GenerateKeyPair()
	require.NoError(t, err)
	handler := core.NewWebhookHandler(&config.WaffoConfig{APIKey: "local-fixture", PrivateKey: pair.PrivateKey, WaffoPublicKey: pair.PublicKey})
	deliver := func(id string) string {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/waffo", nil)
		// handleWaffoPayment is called only after WaffoWebhook verified the payload.
		handleWaffoPayment(c, handler, &core.PaymentNotificationResult{MerchantOrderID: id, OrderStatus: "PAY_SUCCESS", OrderAmount: "1", OrderCurrency: "USD"})
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

func TestRestoredNativeUpstreamSyncPancakeAcknowledgesPermanentVerifiedFailure(t *testing.T) {
	db := restoredFundsDB(t)
	restoredCompliance(t)
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
	order := model.TopUp{UserId: user.Id, TradeNo: "pancake-expired", PaymentProvider: model.PaymentProviderWaffoPancake, PaymentMethod: model.PaymentMethodWaffoPancake, Status: common.TopUpStatusExpired, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
	require.NoError(t, db.Create(&order).Error)
	body, err := json.Marshal(map[string]any{"id": "evt_local", "timestamp": time.Now().UTC().Format(time.RFC3339), "eventType": "order.completed", "eventId": "event_local", "storeId": "local-store", "mode": "test", "data": map[string]any{"orderStatus": "completed", "paymentStatus": "succeeded", "taxAmount": "0", "orderId": "order_local", "orderMerchantExternalId": order.TradeNo, "merchantProvidedBuyerIdentity": service.WaffoPancakeBuyerIdentityFromUserID(user.Id), "currency": "USD", "amount": "1"}})
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
