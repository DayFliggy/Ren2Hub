package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	waffoutils "github.com/waffo-com/waffo-go/utils"
	"gorm.io/gorm"
)

func setupWalletPaymentGuard(t *testing.T, provider, tradeNo string) (*gorm.DB, model.TopUp) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.BillingEvent{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousAffiliate := common.RedisEnabled, common.AffiliateEnabled
	previousQuotaPerUnit := common.QuotaPerUnit
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.AffiliateEnabled = false, false
	common.QuotaPerUnit = 100
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.AffiliateEnabled = previousRedis, previousAffiliate
		common.QuotaPerUnit = previousQuotaPerUnit
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "payment-guard", Quota: 7}).Error)
	order := model.TopUp{
		UserId: 1, Amount: 2, Money: 2, TradeNo: tradeNo,
		PaymentMethod: provider, PaymentProvider: provider, Status: common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(&order).Error)
	return db, order
}

func TestStripePaymentEventsKeepWalletOrdersIsolated(t *testing.T) {
	for _, tc := range []struct {
		name, mode, reference string
		allowed               bool
	}{
		{"one-time wallet", "payment", "ref_wallet", true},
		{"recurring payment", "subscription", "ref_wallet", false},
		{"archived plan reference", "payment", "sub_ref_archived", false},
		{"missing checkout mode", "", "ref_wallet", false},
	} {
		for _, operation := range []string{"complete", "expire", "async_failed"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				db, order := setupWalletPaymentGuard(t, model.PaymentProviderStripe, tc.reference)
				event := stripe.Event{Data: &stripe.EventData{Object: map[string]interface{}{
					"mode": tc.mode, "client_reference_id": tc.reference,
					"status": "expired", "amount_total": "200", "currency": "usd",
				}}}
				wantQuota, wantStatus := 7, common.TopUpStatusPending
				switch operation {
				case "complete":
					fulfillOrder(context.Background(), event, tc.reference, "", "127.0.0.1")
					if tc.allowed {
						wantQuota, wantStatus = 207, common.TopUpStatusSuccess
					}
				case "expire":
					sessionExpired(context.Background(), event)
					if tc.allowed {
						wantStatus = common.TopUpStatusExpired
					}
				case "async_failed":
					sessionAsyncPaymentFailed(context.Background(), event, "127.0.0.1")
					if tc.allowed {
						wantStatus = common.TopUpStatusFailed
					}
				}
				var user model.User
				require.NoError(t, db.First(&user, 1).Error)
				require.NoError(t, db.First(&order, order.Id).Error)
				assert.EqualValues(t, wantQuota, user.Quota)
				assert.EqualValues(t, wantStatus, order.Status)
			})
		}
	}
}

func TestCreemCheckoutKeepsWalletOrdersIsolated(t *testing.T) {
	for _, tc := range []struct {
		name, reference, orderType string
		allowed                    bool
	}{
		{"one-time wallet", "ref_wallet", "onetime", true},
		{"archived one-time plan", "sub_ref_archived", "onetime", false},
		{"recurring payment", "ref_wallet", "recurring", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, order := setupWalletPaymentGuard(t, model.PaymentProviderCreem, tc.reference)
			event := &CreemWebhookEvent{}
			event.Object.RequestId = tc.reference
			event.Object.Order.Status = "paid"
			event.Object.Order.Type = tc.orderType
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/creem/webhook", nil)
			handleCheckoutCompleted(ctx, event)
			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&order, order.Id).Error)
			wantQuota, wantStatus := 7, common.TopUpStatusPending
			if tc.allowed {
				// Creem stores credited quota directly in Amount.
				wantQuota, wantStatus = 9, common.TopUpStatusSuccess
			}
			assert.EqualValues(t, wantQuota, user.Quota)
			assert.EqualValues(t, wantStatus, order.Status)
			assert.EqualValues(t, http.StatusOK, response.Code)
		})
	}
}

func TestWaffoPancakeRejectsArchivedPlanBeforeWalletLookup(t *testing.T) {
	db, order := setupWalletPaymentGuard(t, model.PaymentProviderWaffoPancake, "WAFFO_PANCAKE_SUB-archived")
	_, err := service.ResolveWaffoPancakeTradeNo(&service.WaffoPancakeWebhookEvent{
		Data: service.WaffoPancakeWebhookData{
			OrderMerchantExternalID:       order.TradeNo,
			MerchantProvidedBuyerIdentity: service.WaffoPancakeBuyerIdentityFromUserID(1),
		},
	})
	require.EqualError(t, err, "retired payment order")
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.EqualValues(t, common.TopUpStatusPending, order.Status)
}

func TestEpayWebhookRejectsArchivedPlanWalletCredit(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	previousAddress, previousID, previousKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	previousMethods := operation_setting.PayMethods
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://pay.example.com", "1", "test-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = previousAddress, previousID, previousKey
		operation_setting.PayMethods = previousMethods
	})
	for _, tc := range []struct {
		name, reference string
		allowed         bool
	}{
		{"wallet payment", "USR1NOwallet", true},
		{"archived plan", "SUBUSR1NOarchived", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, order := setupWalletPaymentGuard(t, model.PaymentProviderEpay, tc.reference)
			params := epay.GenerateParams(map[string]string{
				"pid": "1", "out_trade_no": tc.reference, "type": "alipay",
				"trade_no": "provider-order", "trade_status": epay.StatusTradeSuccess, "money": "2.00",
			}, operation_setting.EpayKey)
			form := url.Values{}
			for key, value := range params {
				form.Set(key, value)
			}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(form.Encode()))
			ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			EpayNotify(ctx)
			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&order, order.Id).Error)
			wantQuota, wantStatus, wantBody := 7, common.TopUpStatusPending, "fail"
			if tc.allowed {
				wantQuota, wantStatus, wantBody = 207, common.TopUpStatusSuccess, "success"
			}
			assert.EqualValues(t, wantQuota, user.Quota)
			assert.EqualValues(t, wantStatus, order.Status)
			assert.EqualValues(t, wantBody, response.Body.String())
		})
	}
}

func TestWaffoWebhookRejectsRecurringWalletCredit(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	previousEnabled, previousSandbox := setting.WaffoEnabled, setting.WaffoSandbox
	previousAPIKey, previousPrivateKey, previousPublicCert := setting.WaffoApiKey, setting.WaffoPrivateKey, setting.WaffoPublicCert
	setting.WaffoEnabled, setting.WaffoSandbox = true, false
	setting.WaffoApiKey = "test-api-key"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	setting.WaffoPrivateKey = base64.StdEncoding.EncodeToString(privateDER)
	setting.WaffoPublicCert = base64.StdEncoding.EncodeToString(publicDER)
	t.Cleanup(func() {
		setting.WaffoEnabled, setting.WaffoSandbox = previousEnabled, previousSandbox
		setting.WaffoApiKey, setting.WaffoPrivateKey, setting.WaffoPublicCert = previousAPIKey, previousPrivateKey, previousPublicCert
	})
	for _, tc := range []struct {
		name, marker string
		allowed      bool
	}{
		{"one-time wallet", "", true},
		{"recurring payment", `,"subscriptionInfo":{"subscriptionId":"archived"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, order := setupWalletPaymentGuard(t, model.PaymentProviderWaffo, "WAFFO-wallet")
			body := `{"eventType":"PAYMENT_NOTIFICATION","result":{"merchantOrderId":"WAFFO-wallet","orderStatus":"PAY_SUCCESS"` + tc.marker + `}}`
			signature, err := waffoutils.Sign(body, setting.WaffoPrivateKey)
			require.NoError(t, err)
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/waffo/webhook", strings.NewReader(body))
			ctx.Request.Header.Set("X-SIGNATURE", signature)
			WaffoWebhook(ctx)
			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&order, order.Id).Error)
			wantQuota, wantStatus, wantMessage := 7, common.TopUpStatusPending, "failed"
			if tc.allowed {
				wantQuota, wantStatus, wantMessage = 207, common.TopUpStatusSuccess, "success"
			}
			assert.EqualValues(t, wantQuota, user.Quota)
			assert.EqualValues(t, wantStatus, order.Status)
			assert.JSONEq(t, `{"message":"`+wantMessage+`"}`, response.Body.String())
		})
	}
}
