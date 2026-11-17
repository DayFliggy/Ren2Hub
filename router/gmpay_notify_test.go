package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// The registered GMPay callback accepts the signed parent-order notification
// epusdt sends after a switched-asset payment (empty block_transaction_id):
// it credits the frozen quota once, a replay credits nothing, and a callback
// with another amount or token is refused even when correctly signed.
func TestGmpayNotifyRouteCreditsSwitchedPaymentOnce(t *testing.T) {
	oldDB, oldLog, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	oldType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gmpay-notify.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}, &model.GmpayMerchantVersion{}, &model.GmpayOrder{}, &model.AffiliateRecord{}, &model.AdminQuotaOperation{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.SetMainDatabaseType(oldType)
		common.SetLogDatabaseType(oldLogType)
		sqlDB.Close()
	})
	user := model.User{Username: "gmpay-route", AffCode: "gmpay-route", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "gmpay-route-order", PaymentProvider: model.PaymentProviderGmpay, Status: common.TopUpStatusPending}
	merchant := model.GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant-route", Key: "route-test-key"}
	require.NoError(t, model.CreateGmpayOrder(context.Background(), &order, &model.GmpayOrder{AmountMinor: 730, Currency: "CNY", ExpectedQuota: 500_000, Token: "usdt", Network: "tron"}, &merchant))

	engine := gin.New()
	registerExtensionRoutes(engine.Group("/api"))
	notify := func(change func(map[string]string)) string {
		params := map[string]string{
			"pid": "merchant-route", "trade_id": "parent-trade", "order_id": order.TradeNo, "amount": "7.3",
			"actual_amount": "1", "receive_address": "TParentAddress", "token": "usdt", "block_transaction_id": "", "status": "2",
		}
		change(params)
		signature, err := gmpay.Sign(params, "route-test-key")
		require.NoError(t, err)
		params["signature"] = signature
		body, err := common.Marshal(params)
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/api/user/gmpay/notify", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		return response.Body.String()
	}
	quota := func() int {
		var current model.User
		require.NoError(t, db.First(&current, user.Id).Error)
		return current.Quota
	}
	require.Equal(t, "fail", notify(func(p map[string]string) { p["amount"] = "7.31" }))
	require.Equal(t, "fail", notify(func(p map[string]string) { p["token"] = "usdc" }))
	require.Zero(t, quota())
	require.Equal(t, "success", notify(func(map[string]string) {}))
	require.Equal(t, 500_000, quota())
	require.Equal(t, "success", notify(func(map[string]string) {}), "a replay is acknowledged")
	require.Equal(t, "fail", notify(func(p map[string]string) { p["block_transaction_id"] = "0xlater" }))
	require.Equal(t, 500_000, quota())
	require.Equal(t, common.TopUpStatusSuccess, model.GetTopUpByTradeNo(order.TradeNo).Status)
}
