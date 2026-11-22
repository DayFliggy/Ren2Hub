package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"github.com/QuantumNous/new-api/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/core"
	"github.com/waffo-com/waffo-go/utils"
)

func TestPaymentCallbacksRejectMismatchedReceipts(t *testing.T) {
	for _, provider := range []string{model.PaymentProviderStripe, model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake} {
		t.Run(provider, func(t *testing.T) {
			db := useGmpayControllerDB(t)
			user := model.User{Username: "receipt-user", AffCode: "receipt-user"}
			require.NoError(t, db.Create(&user).Error)
			order := model.TopUp{UserId: user.Id, TradeNo: "receipt-order", PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, ExpectedQuota: 100, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
			require.NoError(t, order.Insert())
			var deliver func(string, string)
			switch provider {
			case model.PaymentProviderStripe:
				send := useStripeWebhookForTest(t)
				deliver = func(amount, currency string) {
					send(stripeCheckoutEvent(fmt.Sprintf(`"mode":"payment","client_reference_id":"receipt-order","amount_subtotal":%s,"currency":"%s"`, amount, currency)))
				}
			case model.PaymentProviderCreem:
				confirmPaymentComplianceForTest(t)
				oldKey, oldProducts, oldSecret := setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret
				t.Cleanup(func() {
					setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = oldKey, oldProducts, oldSecret
				})
				setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = "test-key", `[{"productId":"test"}]`, "test-secret"
				router := gin.New()
				router.POST("/creem", CreemWebhook)
				deliver = func(amount, currency string) {
					body := fmt.Sprintf(`{"id":"test","eventType":"checkout.completed","object":{"status":"completed","request_id":"receipt-order","order":{"id":"order","status":"paid","type":"onetime","amount":%s,"sub_total":%s,"amount_paid":%s,"currency":"%s"}}}`, amount, amount, amount, currency)
					req := httptest.NewRequest(http.MethodPost, "/creem", strings.NewReader(body))
					req.Header.Set(CreemSignatureHeader, generateCreemSignature(body, setting.CreemWebhookSecret))
					router.ServeHTTP(httptest.NewRecorder(), req)
				}
			case model.PaymentProviderWaffoPancake:
				confirmPaymentComplianceForTest(t)
				oldMerchant, oldKey, oldProduct, oldStore := setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID, setting.WaffoPancakeStoreID
				t.Cleanup(func() {
					setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID, setting.WaffoPancakeStoreID = oldMerchant, oldKey, oldProduct, oldStore
				})
				setting.WaffoPancakeMerchantID, setting.WaffoPancakePrivateKey, setting.WaffoPancakeProductID, setting.WaffoPancakeStoreID = "test", "present", "test", "test-store"
				private, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
				require.NoError(t, err)
				t.Setenv("WAFFO_WEBHOOK_TEST_PUBLIC_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})))
				router := gin.New()
				router.POST("/pancake/:env", WaffoPancakeWebhook)
				deliver = func(amount, currency string) {
					body, err := common.Marshal(map[string]any{"id": "evt_test", "timestamp": time.Now().UTC().Format(time.RFC3339), "eventType": "order.completed", "eventId": "test", "storeId": "test-store", "mode": "test", "data": map[string]any{"orderStatus": "completed", "taxAmount": "0", "orderId": "order", "orderMerchantExternalId": order.TradeNo, "merchantProvidedBuyerIdentity": service.WaffoPancakeBuyerIdentityFromUserID(user.Id), "amount": amount, "currency": currency}})
					require.NoError(t, err)
					timestamp := fmt.Sprint(time.Now().UnixMilli())
					digest := sha256.Sum256([]byte(timestamp + "." + string(body)))
					signature, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
					require.NoError(t, err)
					req := httptest.NewRequest(http.MethodPost, "/pancake/test", strings.NewReader(string(body)))
					req.Header.Set("X-Waffo-Signature", "t="+timestamp+",v1="+base64.StdEncoding.EncodeToString(signature))
					router.ServeHTTP(httptest.NewRecorder(), req)
				}
			case model.PaymentProviderWaffo:
				pair, err := utils.GenerateKeyPair()
				require.NoError(t, err)
				handler := core.NewWebhookHandler(&config.WaffoConfig{APIKey: "test", PrivateKey: pair.PrivateKey, WaffoPublicKey: pair.PublicKey})
				deliver = func(amount, currency string) {
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/waffo", nil)
					handleWaffoPayment(c, handler, &core.PaymentNotificationResult{MerchantOrderID: order.TradeNo, OrderStatus: "PAY_SUCCESS", OrderAmount: amount, OrderCurrency: currency})
				}
			}
			wrong, correct := "99", "100"
			if provider == model.PaymentProviderWaffo || provider == model.PaymentProviderWaffoPancake {
				wrong, correct = "0.99", "1"
			}
			for _, receipt := range [][2]string{{wrong, "USD"}, {correct, "EUR"}} {
				deliver(receipt[0], receipt[1])
				require.NoError(t, db.First(&user, user.Id).Error)
				require.Zero(t, user.Quota)
				require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
			}
			deliver(correct, "USD")
			deliver(correct, "USD")
			require.NoError(t, db.First(&user, user.Id).Error)
			require.Equal(t, 100, user.Quota)
		})
	}
}

func TestStripeReceiptUsesFrozenSubtotalBeforeTaxAndPromotion(t *testing.T) {
	for _, tc := range []struct{ name, fields string }{
		{"promotion", `"amount_subtotal":100,"amount_total":80,"currency":"usd","total_details":{"amount_discount":20,"amount_tax":0}`},
		{"tax", `"amount_subtotal":100,"amount_total":120,"currency":"usd","total_details":{"amount_discount":0,"amount_tax":20}`},
		{"source_currency", `"amount_subtotal":92,"amount_total":110,"currency":"eur","currency_conversion":{"amount_subtotal":100,"amount_total":120,"source_currency":"usd","fx_rate":"0.92"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := useGmpayControllerDB(t)
			send := useStripeWebhookForTest(t)
			user := model.User{Username: "stripe-subtotal", AffCode: "stripe-subtotal"}
			require.NoError(t, db.Create(&user).Error)
			order := model.TopUp{UserId: user.Id, TradeNo: "subtotal-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 100, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
			require.NoError(t, order.Insert())
			require.Equal(t, http.StatusOK, send(stripeCheckoutEvent(`"mode":"payment","client_reference_id":"subtotal-order",`+tc.fields)))
			require.NoError(t, db.First(&user, user.Id).Error)
			require.Equal(t, 100, user.Quota)
		})
	}
}

func TestCreemWebhookRequiresListedAmount(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		accepted     bool
	}{
		{"tax_paid_above_amount", `"amount":100,"tax_amount":21,"amount_paid":121`, true},
		{"missing_amount", `"amount_paid":100`, false},
		{"amount_mismatch", `"amount":99,"amount_paid":100`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := useGmpayControllerDB(t)
			confirmPaymentComplianceForTest(t)
			oldKey, oldProducts, oldSecret := setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret
			t.Cleanup(func() {
				setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = oldKey, oldProducts, oldSecret
			})
			setting.CreemApiKey, setting.CreemProducts, setting.CreemWebhookSecret = "test-key", `[{"productId":"test"}]`, "test-secret"
			user := model.User{Username: "creem-paid", AffCode: "creem-paid"}
			require.NoError(t, db.Create(&user).Error)
			order := model.TopUp{UserId: user.Id, TradeNo: "creem-paid-order", PaymentProvider: model.PaymentProviderCreem, PaymentMethod: model.PaymentMethodCreem, Status: common.TopUpStatusPending, ExpectedQuota: 100, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
			require.NoError(t, order.Insert())
			router := gin.New()
			router.POST("/creem", CreemWebhook)
			send := func(fields string) int {
				body := `{"id":"test","eventType":"checkout.completed","object":{"status":"completed","request_id":"creem-paid-order","order":{"id":"order","status":"paid","type":"onetime","currency":"USD",` + fields + `}}}`
				req := httptest.NewRequest(http.MethodPost, "/creem", strings.NewReader(body))
				req.Header.Set(CreemSignatureHeader, generateCreemSignature(body, setting.CreemWebhookSecret))
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return response.Code
			}
			status := send(tc.fields)
			if !tc.accepted {
				require.Equal(t, http.StatusOK, status)
				require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
				require.NoError(t, db.First(&user, user.Id).Error)
				require.Zero(t, user.Quota)
				// A corrected receipt must still be able to complete the pending order.
				status = send(`"amount":100,"amount_paid":100`)
			}
			require.Equal(t, http.StatusOK, status)
			require.Equal(t, http.StatusOK, send(`"amount":100,"amount_paid":100`))
			require.NoError(t, db.First(&user, user.Id).Error)
			require.Equal(t, 100, user.Quota)
			require.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo(order.TradeNo).Status)
		})
	}
}
