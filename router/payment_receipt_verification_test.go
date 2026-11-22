package router

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func paymentReceiptRouter(t *testing.T, provider string, private *rsa.PrivateKey) (*gin.Engine, *gorm.DB) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	oldDB, oldLog, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	oldType := common.MainDatabaseType()
	oldGlobal, oldCritical := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "receipt.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.AuditLog{}, &model.Option{}, &model.AffiliateRecord{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false
	pay := operation_setting.GetPaymentSetting()
	oldPay := *pay
	pay.ComplianceConfirmed = true
	pay.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	stringsToRestore := map[*string]string{}
	set := func(p *string, v string) { stringsToRestore[p] = *p; *p = v }
	set(&setting.StripeApiSecret, "sk_fixture")
	set(&setting.StripeWebhookSecret, "whsec_fixture")
	set(&setting.StripePriceId, "price_fixture")
	set(&setting.CreemApiKey, "fixture")
	set(&setting.CreemProducts, `[{"productId":"fixture"}]`)
	set(&setting.CreemWebhookSecret, "creem-fixture")
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	set(&setting.WaffoMerchantId, "merchant-fixture")
	set(&setting.WaffoSandboxApiKey, "fixture")
	set(&setting.WaffoSandboxPrivateKey, base64.StdEncoding.EncodeToString(privateDER))
	set(&setting.WaffoSandboxPublicCert, base64.StdEncoding.EncodeToString(public))
	oldEnabled, oldSandbox := setting.WaffoEnabled, setting.WaffoSandbox
	setting.WaffoEnabled, setting.WaffoSandbox = true, true
	set(&setting.WaffoPancakeMerchantID, "fixture")
	set(&setting.WaffoPancakePrivateKey, "fixture")
	set(&setting.WaffoPancakeProductID, "fixture")
	set(&setting.WaffoPancakeStoreID, "store-fixture")
	t.Setenv("WAFFO_WEBHOOK_TEST_PUBLIC_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})))
	t.Cleanup(func() {
		for p, v := range stringsToRestore {
			*p = v
		}
		*pay = oldPay
		setting.WaffoEnabled, setting.WaffoSandbox = oldEnabled, oldSandbox
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.SetMainDatabaseType(oldType)
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = oldGlobal, oldCritical
		sqlDB.Close()
	})
	user := model.User{Username: "receipt-user", AffCode: "receipt-user"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "receipt-order", PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, ExpectedQuota: 100, Amount: 10, Money: 10, Currency: "USD", PayAmount: "10"}
	require.NoError(t, order.Insert())
	engine := gin.New()
	SetApiRouter(engine)
	return engine, db
}

func paymentReceiptObject(t *testing.T, provider string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "payment-receipts", provider+".json"))
	require.NoError(t, err)
	var event map[string]any
	require.NoError(t, json.Unmarshal(raw, &event))
	return event
}
func receiptMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		m = m[key].(map[string]any)
	}
	return m
}
func deliverPaymentReceipt(t *testing.T, engine *gin.Engine, provider string, event map[string]any, key *rsa.PrivateKey) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	path := "/api/" + provider + "/webhook"
	if provider == model.PaymentProviderWaffoPancake {
		path = "/api/waffo-pancake/webhook/test"
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	switch provider {
	case model.PaymentProviderStripe:
		signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: raw, Secret: setting.StripeWebhookSecret})
		req.Header.Set("Stripe-Signature", signed.Header)
	case model.PaymentProviderCreem:
		mac := hmac.New(sha256.New, []byte(setting.CreemWebhookSecret))
		mac.Write(raw)
		req.Header.Set("creem-signature", hex.EncodeToString(mac.Sum(nil)))
	default:
		message := string(raw)
		header := "X-SIGNATURE"
		prefix := ""
		if provider == model.PaymentProviderWaffoPancake {
			stamp := fmt.Sprint(time.Now().UnixMilli())
			message = stamp + "." + message
			header = "X-Waffo-Signature"
			prefix = "t=" + stamp + ",v1="
		}
		digest := sha256.Sum256([]byte(message))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		req.Header.Set(header, prefix+base64.StdEncoding.EncodeToString(sig))
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	return response
}

func TestPaymentReceiptRoutesVerifyOfficialContracts(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	type receiptCase struct {
		name     string
		edit     func(map[string]any)
		accepted bool
	}
	for _, provider := range []string{model.PaymentProviderStripe, model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake} {
		cases := []receiptCase{{name: "official_success", accepted: true}}
		switch provider {
		case model.PaymentProviderStripe:
			obj := func(e map[string]any) map[string]any { return receiptMap(e, "data", "object") }
			cases = append(cases, receiptCase{"amount_mismatch", func(e map[string]any) { obj(e)["amount_subtotal"] = 999 }, false}, receiptCase{"currency_mismatch", func(e map[string]any) { obj(e)["currency"] = "eur" }, false}, receiptCase{"missing_subtotal", func(e map[string]any) { delete(obj(e), "amount_subtotal") }, false}, receiptCase{"unpaid", func(e map[string]any) { obj(e)["payment_status"] = "unpaid" }, false}, receiptCase{"async_unpaid", func(e map[string]any) {
				e["type"] = "checkout.session.async_payment_succeeded"
				obj(e)["payment_status"] = "unpaid"
			}, false}, receiptCase{"free", func(e map[string]any) { obj(e)["payment_status"] = "no_payment_required" }, false}, receiptCase{"promotion", func(e map[string]any) { obj(e)["amount_total"] = 800 }, true}, receiptCase{"conversion", func(e map[string]any) {
				obj(e)["currency"] = "eur"
				obj(e)["amount_subtotal"] = 900
				obj(e)["currency_conversion"] = map[string]any{"amount_subtotal": 1000, "source_currency": "usd"}
			}, true}, receiptCase{"async_paid", func(e map[string]any) { e["type"] = "checkout.session.async_payment_succeeded" }, true})
		case model.PaymentProviderCreem:
			order := func(e map[string]any) map[string]any { return receiptMap(e, "object", "order") }
			cases = append(cases, receiptCase{"amount_mismatch", func(e map[string]any) { order(e)["amount"] = 999; order(e)["amount_paid"] = 1000 }, false}, receiptCase{"currency_mismatch", func(e map[string]any) { order(e)["currency"] = "EUR" }, false}, receiptCase{"missing_amount", func(e map[string]any) { delete(order(e), "amount"); order(e)["amount_paid"] = 1000 }, false}, receiptCase{"checkout_pending", func(e map[string]any) { receiptMap(e, "object")["status"] = "pending"; order(e)["amount_paid"] = 1000 }, false}, receiptCase{"order_pending", func(e map[string]any) { order(e)["status"] = "pending" }, false}, receiptCase{"multiple_units", func(e map[string]any) { receiptMap(e, "object")["units"] = 2; order(e)["amount_paid"] = 1000 }, false}, receiptCase{"missing_units", func(e map[string]any) { delete(receiptMap(e, "object"), "units") }, true}, receiptCase{"discount", func(e map[string]any) {
				order(e)["amount_paid"] = 800
				order(e)["amount_due"] = 800
				order(e)["discount_amount"] = 200
			}, true})
		case model.PaymentProviderWaffo:
			result := func(e map[string]any) map[string]any { return receiptMap(e, "result") }
			cases = append(cases,
				receiptCase{"missing_order", func(e map[string]any) { delete(result(e), "merchantOrderId") }, false},
				receiptCase{"missing_configured_merchant", func(e map[string]any) { setting.WaffoMerchantId = "" }, false},
				receiptCase{"amount_mismatch", func(e map[string]any) { result(e)["orderAmount"] = "9.99" }, false}, receiptCase{"currency_mismatch", func(e map[string]any) { result(e)["orderCurrency"] = "EUR" }, false}, receiptCase{"wrong_merchant", func(e map[string]any) { receiptMap(e, "result", "merchantInfo")["merchantId"] = "foreign" }, false}, receiptCase{"empty_merchant", func(e map[string]any) { receiptMap(e, "result", "merchantInfo")["merchantId"] = "" }, false}, receiptCase{"discount", func(e map[string]any) { result(e)["originalAmount"] = "12" }, false})
		case model.PaymentProviderWaffoPancake:
			data := func(e map[string]any) map[string]any { return receiptMap(e, "data") }
			price := func(e map[string]any) map[string]any { return receiptMap(e, "data", "listPrice") }
			cases = append(cases,
				receiptCase{"missing_discount", func(e map[string]any) { delete(price(e), "discountAmount") }, false},
				receiptCase{"missing_store_config", func(e map[string]any) { setting.WaffoPancakeStoreID = "" }, false},
				receiptCase{"missing_status", func(e map[string]any) { delete(data(e), "orderStatus") }, false},
				receiptCase{"legacy_missing_tax", func(e map[string]any) { delete(data(e), "listPrice"); delete(data(e), "taxAmount") }, false},
				receiptCase{"amount_mismatch", func(e map[string]any) { price(e)["subtotal"] = "9.99" }, false}, receiptCase{"currency_mismatch", func(e map[string]any) { data(e)["currency"] = "EUR" }, false}, receiptCase{"cross_store", func(e map[string]any) { e["storeId"] = "foreign" }, false}, receiptCase{"pending", func(e map[string]any) { data(e)["orderStatus"] = "pending" }, false}, receiptCase{"payment_failed", func(e map[string]any) { data(e)["paymentStatus"] = "failed" }, false}, receiptCase{"discount", func(e map[string]any) { price(e)["discountAmount"] = "1" }, false}, receiptCase{"charged_mismatch", func(e map[string]any) { data(e)["chargedAmount"] = "9.99" }, false}, receiptCase{"tax", func(e map[string]any) {
					price(e)["total"] = "12"
					price(e)["taxAmount"] = "2"
					data(e)["chargedAmount"] = "12"
					data(e)["amount"] = "12"
				}, true}, receiptCase{"legacy", func(e map[string]any) { delete(data(e), "listPrice") }, true}, receiptCase{"legacy_tax", func(e map[string]any) { delete(data(e), "listPrice"); data(e)["taxAmount"] = "2" }, false}, receiptCase{"missing_payment_status", func(e map[string]any) { delete(data(e), "paymentStatus") }, true})
		}
		for _, tc := range cases {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				engine, db := paymentReceiptRouter(t, provider, private)
				event := paymentReceiptObject(t, provider)
				if tc.edit != nil {
					tc.edit(event)
				}
				response := deliverPaymentReceipt(t, engine, provider, event, private)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var user model.User
				require.NoError(t, db.First(&user).Error)
				saved := model.GetTopUpByTradeNo("receipt-order")
				require.NotNil(t, saved)
				if tc.accepted {
					require.Equal(t, 100, user.Quota)
					require.Equal(t, common.TopUpStatusSuccess, saved.Status)
					require.Equal(t, 100, saved.CreditedQuota)
					require.Equal(t, http.StatusOK, deliverPaymentReceipt(t, engine, provider, event, private).Code)
					require.NoError(t, db.First(&user).Error)
					require.Equal(t, 100, user.Quota)
				} else {
					require.Zero(t, user.Quota)
					require.Zero(t, saved.CreditedQuota)
					require.Equal(t, common.TopUpStatusPending, saved.Status)
					var audits int64
					require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "payment.callback_rejected").Count(&audits).Error)
					require.Positive(t, audits)
				}
			})
		}
	}
}

func TestPaymentReceiptRoutesPreserveRetryAndSignatureContracts(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, provider := range []string{model.PaymentProviderStripe, model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake} {
		t.Run(provider, func(t *testing.T) {
			engine, db := paymentReceiptRouter(t, provider, private)
			event := paymentReceiptObject(t, provider)
			path := "/api/" + provider + "/webhook"
			if provider == model.PaymentProviderWaffoPancake {
				path = "/api/waffo-pancake/webhook/test"
			}
			raw, err := json.Marshal(event)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			unsigned := httptest.NewRecorder()
			engine.ServeHTTP(unsigned, req)
			require.GreaterOrEqual(t, unsigned.Code, 400)
			var audits int64
			require.NoError(t, db.Model(&model.AuditLog{}).Count(&audits).Error)
			require.Zero(t, audits, "an unauthenticated payload must not create a trusted callback rejection")
			failure := fmt.Errorf("injected wallet outage")
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("receipt_wallet_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "users" {
					tx.AddError(failure)
				}
			}))
			response := deliverPaymentReceipt(t, engine, provider, event, private)
			if provider == model.PaymentProviderStripe {
				require.Equal(t, 200, response.Code, "FUND-2 remains the upstream ACK behavior")
			} else {
				require.Equal(t, 500, response.Code, response.Body.String())
			}
			var user model.User
			require.NoError(t, db.First(&user).Error)
			require.Zero(t, user.Quota)
			require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo("receipt-order").Status)
			require.NoError(t, db.Model(&model.AuditLog{}).Count(&audits).Error)
			require.Zero(t, audits, "temporary storage failures are not permanent receipt mismatches")
			require.NoError(t, db.Callback().Update().Remove("receipt_wallet_failure"))
			require.Equal(t, 200, deliverPaymentReceipt(t, engine, provider, event, private).Code)
			require.NoError(t, db.First(&user).Error)
			require.Equal(t, 100, user.Quota)
		})
	}
}

// These upstream behaviors remain deliberately registered, so receipt hardening
// must not turn a known ACK or state-machine defect into an accidental claim of recovery.
func TestPaymentReceiptRoutesRetainRegisteredUpstreamFailures(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	t.Run("waffo_intermediate_notification_closes_pending_order", func(t *testing.T) {
		engine, db := paymentReceiptRouter(t, model.PaymentProviderWaffo, private)
		event := paymentReceiptObject(t, model.PaymentProviderWaffo)
		receiptMap(event, "result")["orderStatus"] = "PAY_IN_PROGRESS"
		response := deliverPaymentReceipt(t, engine, model.PaymentProviderWaffo, event, private)
		require.Equal(t, http.StatusOK, response.Code)
		require.JSONEq(t, `{"message":"success"}`, response.Body.String())
		var user model.User
		require.NoError(t, db.First(&user).Error)
		require.Zero(t, user.Quota)
		require.Equal(t, common.TopUpStatusFailed, model.GetTopUpByTradeNo("receipt-order").Status)
		var audits int64
		require.NoError(t, db.Model(&model.AuditLog{}).Count(&audits).Error)
		require.Zero(t, audits)
		// A subsequent paid notification cannot credit the already failed order.
		receiptMap(event, "result")["orderStatus"] = "PAY_SUCCESS"
		require.Equal(t, http.StatusOK, deliverPaymentReceipt(t, engine, model.PaymentProviderWaffo, event, private).Code)
		require.NoError(t, db.First(&user).Error)
		require.Zero(t, user.Quota)
		require.Equal(t, common.TopUpStatusFailed, model.GetTopUpByTradeNo("receipt-order").Status)
	})
	t.Run("pancake_lookup_outage_is_acknowledged", func(t *testing.T) {
		engine, db := paymentReceiptRouter(t, model.PaymentProviderWaffoPancake, private)
		event := paymentReceiptObject(t, model.PaymentProviderWaffoPancake)
		failedLookups := 0
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("pancake_lookup_outage", func(tx *gorm.DB) {
			if tx.Statement.Table == "top_ups" {
				failedLookups++
				tx.AddError(fmt.Errorf("injected temporary order lookup outage"))
			}
		}))
		response := deliverPaymentReceipt(t, engine, model.PaymentProviderWaffoPancake, event, private)
		require.NoError(t, db.Callback().Query().Remove("pancake_lookup_outage"))
		require.Positive(t, failedLookups, "the signed callback must reach the unavailable order lookup")
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "OK", response.Body.String())
		var user model.User
		require.NoError(t, db.First(&user).Error)
		require.Zero(t, user.Quota)
		require.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo("receipt-order").Status)
		var audits int64
		require.NoError(t, db.Model(&model.AuditLog{}).Count(&audits).Error)
		require.Zero(t, audits)
		// The receipt itself is valid: redelivery after the transient outage credits it.
		require.Equal(t, http.StatusOK, deliverPaymentReceipt(t, engine, model.PaymentProviderWaffoPancake, event, private).Code)
		require.NoError(t, db.First(&user).Error)
		require.Equal(t, 100, user.Quota)
	})
}
