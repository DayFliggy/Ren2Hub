package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupEpayControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.EpayOrder{}, &model.EpayMerchantVersion{}, &model.User{}, &model.TopUp{}, &model.BillingEvent{}, &model.Log{}, &model.AuditLog{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousAffiliate := common.RedisEnabled, common.AffiliateEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.AffiliateEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.AffiliateEnabled = previousRedis, previousAffiliate
		common.SetDatabaseTypes(previousMainType, previousLogType)
		require.NoError(t, connection.Close())
	})
	options := map[string]string{
		"PayAddress": "https://pay.example.test", "EpayId": "1", "EpayKey": "test-key",
		"Price": "1", "MinTopUp": "1", "PayMethods": `[{"name":"支付宝","type":"alipay"}]`,
		"TopupGroupRatio": `{"default":1}`, "payment_setting.amount_discount": "{}",
		"payment_setting.compliance_confirmed": "true", "payment_setting.compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"general_setting.quota_display_type": "USD", "ServerAddress": "https://shop.example.test",
	}
	for key, value := range options {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "epay-user", Group: "default", Quota: 7, Status: common.UserStatusEnabled}).Error)
	return db
}

func TestEpayGatewayRetriesAfterSettlementFailure(t *testing.T) {
	for _, failure := range []string{"settlement paused", "funds transaction failed", "administrator completes first"} {
		t.Run(failure, func(t *testing.T) {
			db := setupEpayControllerTestDB(t)
			order, err := service.CreateEpayTopUp(context.Background(), 1, service.EpayQuoteRequest{Amount: 1, AmountUnit: "USD", PaymentMethod: "alipay"})
			require.NoError(t, err)
			params := map[string]string{
				"pid": "1", "out_trade_no": order.Order.TradeNo, "trade_no": "gateway-retry",
				"type": "alipay", "money": order.Quote.PayAmount, "trade_status": epay.StatusTradeSuccess, "sign_type": "MD5",
			}
			params["sign"] = epay.Sign(params, "test-key")
			form := url.Values{}
			for key, value := range params {
				form.Set(key, value)
			}
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.GET("/notify", EpayNotify)
			engine.POST("/notify", EpayNotify)
			notify := func(method string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(method, "/notify", strings.NewReader(form.Encode()))
				if method == http.MethodGet {
					request = httptest.NewRequest(method, "/notify?"+form.Encode(), nil)
				} else {
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)
				return response
			}
			switch failure {
			case "settlement paused":
				require.NoError(t, db.Create(&model.Option{Key: "payment_setting.epay_settlement_enabled", Value: "false"}).Error)
			case "funds transaction failed":
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reject_epay_credit", func(tx *gorm.DB) {
					if tx.Statement.Table == "billing_events" {
						tx.AddError(errors.New("injected ledger failure"))
					}
				}))
				t.Cleanup(func() { db.Callback().Create().Remove("reject_epay_credit") })
			case "administrator completes first":
				require.NoError(t, model.ManualCompleteTopUp(order.Order.TradeNo, ""))
			}
			if failure != "administrator completes first" {
				response := notify(http.MethodPost)
				assert.Equal(t, "fail", response.Body.String(), "the gateway must receive a failed acknowledgement so it can retry")
				var user model.User
				require.NoError(t, db.First(&user, 1).Error)
				assert.Equal(t, common.Quota(7), user.Quota)
				var snapshot model.EpayOrder
				require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
				assert.Nil(t, snapshot.GatewayTradeNo)
				var stored model.TopUp
				require.NoError(t, db.First(&stored, order.Order.Id).Error)
				assert.Equal(t, common.TopUpStatusPending, stored.Status)
				var count int64
				require.NoError(t, db.Model(&model.BillingEvent{}).Count(&count).Error)
				assert.Zero(t, count)
				if failure == "settlement paused" {
					require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: "payment_setting.epay_settlement_enabled"}).Update("value", "true").Error)
				} else {
					require.NoError(t, db.Callback().Create().Remove("reject_epay_credit"))
				}
			}
			for _, method := range []string{http.MethodPost, http.MethodGet} {
				response := notify(method)
				assert.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "success", response.Body.String())
			}
			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			assert.Equal(t, common.Quota(500007), user.Quota)
			var stored model.TopUp
			require.NoError(t, db.First(&stored, order.Order.Id).Error)
			assert.Equal(t, common.TopUpStatusSuccess, stored.Status)
			assert.Equal(t, common.Quota(500000), stored.CreditedQuota)
			var snapshot model.EpayOrder
			require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
			require.NotNil(t, snapshot.GatewayTradeNo)
			assert.Equal(t, "gateway-retry", *snapshot.GatewayTradeNo)
			var count int64
			require.NoError(t, db.Model(&model.BillingEvent{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestAdminEpayCompletionAuditIdentifiesOrderAndOperator(t *testing.T) {
	db := setupEpayControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "payment-admin", AffCode: "payment-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	order, err := service.CreateEpayTopUp(context.Background(), 1, service.EpayQuoteRequest{Amount: 1, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.NoError(t, err)
	body, err := common.Marshal(AdminCompleteTopupRequest{TradeNo: order.Order.TradeNo})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	for range 2 {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup/complete", strings.NewReader(string(body)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", 2)
		c.Set("username", "payment-admin")
		c.Set("role", common.RoleAdminUser)
		AdminCompleteTopUp(c)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.JSONEq(t, `{"success":true,"message":"","data":null}`, response.Body.String())
	}
	var entries []model.AuditLog
	require.NoError(t, db.Where("category = ?", model.AuditCategoryOperation).Order("id").Find(&entries).Error)
	require.Len(t, entries, 2, "each successful administrative request must identify the target order")
	for _, entry := range entries {
		assert.Equal(t, "user.topup_complete", entry.Action)
		assert.Equal(t, "payment-admin", entry.Username)
		assert.Equal(t, 2, entry.UserId)
		encoded, err := common.Marshal(entry.Other.Op.Params["trade_no"])
		require.NoError(t, err)
		var trade string
		require.NoError(t, common.Unmarshal(encoded, &trade))
		assert.Equal(t, order.Order.TradeNo, trade)
		assert.Contains(t, entry.Content, order.Order.TradeNo)
	}
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.EqualValues(t, 1, events, "repeated completion must only credit once")
}
