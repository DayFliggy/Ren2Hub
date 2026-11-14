package controller

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func useGmpayControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldLog, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	oldType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
	common.SQLitePath = filepath.Join(t.TempDir(), "payments.db")
	common.IsMasterNode = false
	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitDB())
	db := model.DB
	db.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.TopUp{}, &model.GmpayOrder{}, &model.GmpayMerchantVersion{}, &model.Log{}, &model.Ticket{}, &model.TicketMessage{}, &model.TicketAttachment{}, &model.AffiliateRecord{}, &model.AdminQuotaOperation{},
		&model.TopUpDiscount{}, &model.TopUpDiscountConfigVersion{}, &model.TopUpDiscountUsageHourly{}))
	service.ResetTopUpDiscountCaches()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.SQLitePath, common.IsMasterNode = oldPath, oldMaster
		common.RedisEnabled = oldRedis
		common.SetMainDatabaseType(oldType)
		common.SetLogDatabaseType(oldLogType)
		service.ResetTopUpDiscountCaches()
		sqlDB.Close()
	})
	return db
}

func TestGmpayUsesNativeAmountQuotaAndCNY(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "payer", AffCode: "payer"}
	require.NoError(t, db.Create(&user).Error)
	oldPayment, oldPrice, oldDisplay := *operation_setting.GetPaymentSetting(), operation_setting.Price, operation_setting.GetQuotaDisplayType()
	oldAddress, oldCallback := system_setting.ServerAddress, operation_setting.CustomCallbackAddress
	oldTransport := http.DefaultTransport
	t.Cleanup(func() {
		*operation_setting.GetPaymentSetting() = oldPayment
		operation_setting.Price = oldPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		system_setting.ServerAddress = oldAddress
		operation_setting.CustomCallbackAddress = oldCallback
		http.DefaultTransport = oldTransport
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	operation_setting.Price = 7.3
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	system_setting.ServerAddress = "https://console.example.test"
	operation_setting.CustomCallbackAddress = "https://callback.example.test"
	var sent map[string]string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		sent = map[string]string{}
		for k, v := range r.PostForm {
			sent[k] = v[0]
		}
		signature, err := gmpay.Sign(sent, "test-secret")
		require.NoError(t, err)
		assert.Equal(t, signature, sent["signature"])
		body, err := common.Marshal(map[string]any{"status_code": 200, "data": map[string]any{"trade_id": "receipt-1", "order_id": sent["order_id"], "amount": sent["amount"], "currency": "CNY", "token": "USDT", "actual_amount": "1.12", "receive_address": "address", "status": 1, "expiration_time": 1893456000, "payment_url": "https://cashier.example.test/pay"}})
		require.NoError(t, err)
		_, err = w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()
	http.DefaultTransport = server.Client().Transport
	require.NoError(t, service.SaveGmpayConfig(context.Background(), setting.GmpayConfig{PayAddress: server.URL, MerchantID: "merchant", SecretKey: "test-secret", Enabled: true, Assets: []setting.GmpayAsset{{Name: "USDT Tron", Token: "usdt", Network: "tron"}}}))
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	engine.POST("/api/user/amount", RequestAmount)
	engine.POST("/api/user/gmpay/pay", GmpayPay)
	// A 10% fixed top-up discount lowers the checkout; credit stays full.
	discount := model.DefaultTopUpDiscountConfig()
	discount.CapRate, discount.FixedRate = "0.1", "0.1"
	discountData, err := common.Marshal(discount)
	require.NoError(t, err)
	_, err = model.SaveTopUpDiscountConfig(discountData, 0, 1, time.Now())
	require.NoError(t, err)
	quoteRequest := httptest.NewRequest(http.MethodPost, "/api/user/amount", strings.NewReader(`{"amount":750001}`))
	quoteRequest.Header.Set("Content-Type", "application/json")
	quoteResponse := httptest.NewRecorder()
	engine.ServeHTTP(quoteResponse, quoteRequest)
	var quote struct {
		Message string `json:"message"`
		Data    string `json:"data"`
		Quote   struct {
			ExpectedQuota int    `json:"expected_quota"`
			Currency      string `json:"currency"`
			ListPayAmount string `json:"list_pay_amount"`
			Factor        string `json:"factor"`
		} `json:"quote"`
	}
	require.NoError(t, common.Unmarshal(quoteResponse.Body.Bytes(), &quote))
	require.Equal(t, "success", quote.Message, quoteResponse.Body.String())
	// Native TOKENS pricing uses the untruncated 1.500002 units (upstream
	// behavior); the credit is truncated to one whole unit.
	assert.Equal(t, "9.86", quote.Data, "1.500002 × 7.3 × 0.9")
	assert.Equal(t, "10.95", quote.Quote.ListPayAmount)
	assert.Equal(t, 500000, quote.Quote.ExpectedQuota, "TOKENS input is truncated to whole units before pricing")
	assert.Equal(t, "CNY", quote.Quote.Currency)
	assert.Equal(t, "0.9", quote.Quote.Factor)

	stale := httptest.NewRequest(http.MethodPost, "/api/user/gmpay/pay", strings.NewReader(`{"amount":750001,"payment_method":"gmpay:usdt.tron","expected_pay_amount":"10.95","expected_currency":"CNY"}`))
	stale.Header.Set("Content-Type", "application/json")
	staleResponse := httptest.NewRecorder()
	engine.ServeHTTP(staleResponse, stale)
	assert.Contains(t, staleResponse.Body.String(), `"code":"topup_quote_changed"`)
	assert.Contains(t, staleResponse.Body.String(), `"pay_amount":"9.86"`)
	assert.Nil(t, sent, "a changed price creates no gateway order")
	var orders int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders, "a changed price creates no local order")

	request := httptest.NewRequest(http.MethodPost, "/api/user/gmpay/pay", strings.NewReader(`{"amount":750001,"payment_method":"gmpay:usdt.tron","expected_pay_amount":"9.860","expected_currency":"cny"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	var result struct {
		Message string `json:"message"`
		URL     string `json:"url"`
		Data    struct {
			TradeNo       string `json:"trade_no"`
			ExpectedQuota int    `json:"expected_quota"`
			PayAmount     string `json:"pay_amount"`
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, "success", result.Message, response.Body.String())
	assert.Equal(t, "cny", sent["currency"])
	assert.Equal(t, quote.Data, sent["amount"])
	assert.Equal(t, quote.Data, result.Data.PayAmount)
	assert.Equal(t, "https://callback.example.test/api/user/gmpay/notify", sent["notify_url"])
	assert.Equal(t, "https://console.example.test/wallet", sent["redirect_url"])
	assert.Equal(t, 500000, result.Data.ExpectedQuota, "native TOKENS normalization remains authoritative")
	order, payment, _, err := model.GetGmpayOrder(context.Background(), result.Data.TradeNo)
	require.NoError(t, err)
	assert.EqualValues(t, 1, order.Amount)
	assert.Equal(t, 500000, payment.ExpectedQuota)
	assert.Equal(t, "CNY", payment.Currency)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota, "creating a checkout never credits a wallet")
	record, err := model.GetTopUpDiscountRecord(order.Id)
	require.NoError(t, err)
	require.NotNil(t, record, "the decision is stored with the order")
	assert.Equal(t, "10.9500146", record.ListPayAmount)
	assert.Contains(t, record.Decision, `"factor":"0.9"`)
	var stored model.TopUp
	require.NoError(t, db.First(&stored, order.Id).Error)
	assert.Equal(t, quote.Quote.ExpectedQuota, stored.ExpectedQuota, "order, snapshot and quote credit the same quota")
	assert.Equal(t, quote.Quote.ExpectedQuota, stored.AffiliateBaseQuota, "the affiliate base stays the full credited quota")
}

func TestGmpayOrderAndConfigPrivacy(t *testing.T) {
	db := useGmpayControllerDB(t)
	config := setting.GmpayConfig{PayAddress: "https://gateway.example.test", MerchantID: "merchant", SecretKey: "private-secret", Enabled: true, Assets: []setting.GmpayAsset{{Name: "USDT", Token: "usdt", Network: "tron"}}}
	require.NoError(t, service.SaveGmpayConfig(context.Background(), config))
	config.SecretKey = ""
	config.Enabled = false
	require.NoError(t, service.SaveGmpayConfig(context.Background(), config))
	saved, err := service.LoadGmpayConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "private-secret", saved.SecretKey)
	order := model.TopUp{UserId: 1, TradeNo: "private-order", PaymentProvider: model.PaymentProviderGmpay, Status: common.TopUpStatusPending}
	require.NoError(t, model.CreateGmpayOrder(context.Background(), &order, &model.GmpayOrder{ExpectedQuota: 500000, AmountMinor: 730, Currency: "CNY", Token: "usdt", Network: "tron"}, &model.GmpayMerchantVersion{GatewayURL: config.PayAddress, MerchantID: config.MerchantID, Key: saved.SecretKey}))
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 2) })
	engine.GET("/order/:trade_no", GetGmpayOrder)
	engine.GET("/config", GetGmpayConfig)
	for _, path := range []string{"/config", "/order/private-order"} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.NotContains(t, response.Body.String(), "private-secret")
		if strings.HasPrefix(path, "/order") {
			assert.NotContains(t, response.Body.String(), "expected_quota")
		}
	}
	var count int64
	require.NoError(t, db.Model(&model.GmpayMerchantVersion{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestGmpayCreationFailureResponsePreservesDurableIdentity(t *testing.T) {
	for _, status := range []string{model.GmpayStatusUnknown, model.GmpayStatusFailed, model.GmpayStatusSettled} {
		t.Run(status, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			gmpayCreationError(c, &service.GmpayCreatedOrder{
				Order:   &model.TopUp{TradeNo: "GPtest", UserId: 42},
				Payment: &model.GmpayOrder{Status: status, MerchantVersionID: "private-version", LastFailure: "TRANSPORT"},
			})
			var payload map[string]any
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.Equal(t, "error", payload["message"], "a settled callback does not turn failed creation into success")
			assert.Equal(t, "GPtest", payload["trade_no"])
			assert.Equal(t, status, payload["creation_status"])
			assert.Equal(t, "Failed to create the payment order", payload["data"])
			assert.NotContains(t, response.Body.String(), "private-version")
			assert.NotContains(t, payload, "user_id")
		})
	}
	for _, order := range []*service.GmpayCreatedOrder{nil, {Order: &model.TopUp{TradeNo: "GPtest"}}} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		gmpayCreationError(c, order)
		var payload map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
		assert.NotContains(t, payload, "creation_status", "do not fabricate a state when its read failed")
		if order == nil {
			assert.NotContains(t, payload, "trade_no")
		}
	}
}

func TestGmpayRejectsInvalidQuotaScaleBeforePricing(t *testing.T) {
	previous := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		common.QuotaPerUnit = scale
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/user/gmpay/pay", strings.NewReader(`{"amount":1,"payment_method":"gmpay:usdt.tron"}`))
		require.NotPanics(t, func() { GmpayPay(c) })
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), `"message":"error"`)
		require.Contains(t, response.Body.String(), "Quota per unit must be greater than 0")
	}
}
