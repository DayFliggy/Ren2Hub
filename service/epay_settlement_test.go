package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func epayServiceTestDB(t *testing.T, gateway string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.TopUp{}, &model.EpayOrder{}, &model.EpayMerchantVersion{}, &model.BillingEvent{}, &model.Log{}))
	previousDB, previousLog := model.DB, model.LOG_DB
	previousKind, previousRedis, previousAffiliate := common.MainDatabaseType(), common.RedisEnabled, common.AffiliateEnabled
	model.DB, model.LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled, common.AffiliateEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLog
		common.SetMainDatabaseType(previousKind)
		common.RedisEnabled, common.AffiliateEnabled = previousRedis, previousAffiliate
		require.NoError(t, connection.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "epay-buyer", Group: "default", AffCode: "epay-buyer"}).Error)
	for key, value := range map[string]string{
		"PayAddress": gateway, "EpayId": "1", "EpayKey": "test-only-key", "Price": "7.3", "MinTopUp": "1",
		"ServerAddress": "https://wallet.example.test", "PayMethods": `[{"name":"Alipay","type":"alipay"}]`,
		"payment_setting.compliance_confirmed": "true", "payment_setting.compliance_terms_version": "v1",
	} {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	return db
}

func setEpayTestOption(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	require.NoError(t, db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model.Option{Key: key, Value: value}).Error)
}

func epayTestNotification(order *EpayCreatedOrder, tradeNo, key string) map[string]string {
	params := map[string]string{"pid": "1", "out_trade_no": order.Order.TradeNo, "trade_no": tradeNo, "type": "alipay", "money": order.Quote.PayAmount, "trade_status": epay.StatusTradeSuccess, "sign_type": "MD5"}
	params["sign"] = epay.Sign(params, key)
	return params
}

func TestEpayOrderCreationFreezesQuoteAndMerchant(t *testing.T) {
	db := epayServiceTestDB(t, "https://pay.example.test")
	ctx := context.Background()
	request := EpayQuoteRequest{Amount: 750000, AmountUnit: "TOKENS", PaymentMethod: "alipay"}
	quote, _, err := QuoteEpay(ctx, 1, request)
	require.NoError(t, err)
	request.QuoteFingerprint = quote.QuoteFingerprint
	setEpayTestOption(t, db, "Price", "8")
	_, err = CreateEpayTopUp(ctx, 1, request)
	var changed *EpayError
	require.ErrorAs(t, err, &changed)
	assert.Equal(t, "QUOTE_CHANGED", changed.Code)
	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count)
	setEpayTestOption(t, db, "Price", "7.3")
	order, err := CreateEpayTopUp(ctx, 1, request)
	require.NoError(t, err)
	assert.Equal(t, "10.95", order.Params["money"])
	assert.Equal(t, order.Params["sign"], epay.Sign(order.Params, "test-only-key"))
	_, snapshot, merchant, err := model.GetEpayOrder(ctx, order.Order.TradeNo)
	require.NoError(t, err)
	assert.EqualValues(t, 750000, snapshot.ExpectedQuota)
	assert.EqualValues(t, 1095, snapshot.AmountMinor)
	assert.NotEmpty(t, merchant.ID)
	setEpayTestOption(t, db, "EpayKey", "rotated-key")
	setEpayTestOption(t, db, "PayMethods", "[]")
	setEpayTestOption(t, db, "general_setting.quota_display_type", "USD")
	require.Error(t, SettleEpayNotification(ctx, epayTestNotification(order, "gateway-1", "rotated-key"), ""))
	callback := epayTestNotification(order, "gateway-1", "test-only-key")
	require.NoError(t, SettleEpayNotification(ctx, callback, ""))
	require.NoError(t, SettleEpayNotification(ctx, callback, ""))
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 750000, user.Quota)
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestEpayExistingQuoteInvalidatesWhenMethodOrMinimumChanges(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"PayMethods", "[]"},
		{"MinTopUp", "20"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			db := epayServiceTestDB(t, "https://pay.example.test")
			request := EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"}
			quote, _, err := QuoteEpay(context.Background(), 1, request)
			require.NoError(t, err)
			request.QuoteFingerprint = quote.QuoteFingerprint
			setEpayTestOption(t, db, tc.key, tc.value)
			_, err = CreateEpayTopUp(context.Background(), 1, request)
			var changed *EpayError
			require.ErrorAs(t, err, &changed)
			assert.Equal(t, "QUOTE_CHANGED", changed.Code)
		})
	}
}

func TestEpayPauseDuringGatewayQueryPreventsSettlement(t *testing.T) {
	var db *gorm.DB
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model.Option{Key: "payment_setting.epay_settlement_enabled", Value: "false"}).Error
		assert.NoError(t, err)
		body, err := common.Marshal(map[string]any{"code": 1, "status": 1, "pid": "1", "out_trade_no": r.PostForm.Get("out_trade_no"), "trade_no": "gateway-paused", "type": "alipay", "money": "7.30"})
		assert.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	db = epayServiceTestDB(t, server.URL)
	order, err := CreateEpayTopUp(context.Background(), 1, EpayQuoteRequest{Amount: 1, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.NoError(t, err)
	_, err = (EpayPaymentService{Client: epay.NewClient(server.Client())}).VerifyOrder(context.Background(), order.Order.TradeNo, 1, "")
	var failure *EpayError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "SETTLEMENT_PAUSED", failure.Code)
	var snapshot model.EpayOrder
	require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
	assert.Equal(t, "SETTLEMENT_PAUSED", snapshot.LastCheckError)
	assert.Nil(t, snapshot.GatewayTradeNo)
	stored := model.GetTopUpByTradeNo(order.Order.TradeNo)
	require.NotNil(t, stored)
	assert.Equal(t, common.TopUpStatusPending, stored.Status)
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.Zero(t, events)
}

func TestEpayLocalGatewayQueryCallbackRace(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		assert.Equal(t, "test-only-key", r.PostForm.Get("key"))
		started <- struct{}{}
		<-release
		body, err := common.Marshal(map[string]any{"code": 1, "status": 1, "pid": "1", "out_trade_no": r.PostForm.Get("out_trade_no"), "trade_no": "gateway-race", "type": "alipay", "money": "7.30"})
		assert.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	db := epayServiceTestDB(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	order, err := CreateEpayTopUp(ctx, 1, EpayQuoteRequest{Amount: 1, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.NoError(t, err)
	service := EpayPaymentService{Client: epay.NewClient(server.Client())}
	_, err = service.VerifyOrder(ctx, order.Order.TradeNo, 2, "")
	require.Error(t, err, "another user must not start a query")
	result := make(chan error, 1)
	go func() { _, err := service.VerifyOrder(ctx, order.Order.TradeNo, 1, ""); result <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	setEpayTestOption(t, db, "EpayKey", "rotated-key")
	require.NoError(t, SettleEpayNotification(ctx, epayTestNotification(order, "gateway-race", "test-only-key"), ""))
	close(release)
	require.NoError(t, <-result)
	var count int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	var snapshot model.EpayOrder
	require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
	assert.Zero(t, snapshot.LeaseUntil)
	assert.NotZero(t, snapshot.CheckedAt)
}

func TestEpayQueryFailureKeepsPendingAndThrottles(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	db := epayServiceTestDB(t, server.URL)
	ctx := context.Background()
	order, err := CreateEpayTopUp(ctx, 1, EpayQuoteRequest{Amount: 1, PaymentMethod: "alipay"})
	require.NoError(t, err)
	service := EpayPaymentService{Client: epay.NewClient(server.Client())}
	_, err = service.VerifyOrder(ctx, order.Order.TradeNo, 1, "")
	require.Error(t, err)
	_, err = service.VerifyOrder(ctx, order.Order.TradeNo, 1, "")
	require.ErrorIs(t, err, model.ErrEpayCheckThrottled)
	assert.EqualValues(t, 1, calls.Load())
	var snapshot model.EpayOrder
	require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
	assert.Equal(t, "QUERY_FAILED", snapshot.LastCheckError)
	assert.Zero(t, snapshot.LeaseUntil)
	assert.GreaterOrEqual(t, snapshot.NextCheckAt, snapshot.CheckedAt+60)
	stored := model.GetTopUpByTradeNo(order.Order.TradeNo)
	require.NotNil(t, stored)
	assert.Equal(t, common.TopUpStatusPending, stored.Status)
	setEpayTestOption(t, db, "payment_setting.epay_settlement_enabled", "false")
	require.Error(t, SettleEpayNotification(ctx, epayTestNotification(order, "gateway-paused", "test-only-key"), ""))
}

func TestEpayLocalGatewayMissedNotificationRecovery(t *testing.T) {
	var calls, active, maximum atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); value > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, value) {
				break
			}
		}
		calls.Add(1)
		assert.NoError(t, r.ParseForm())
		orderNo := r.PostForm.Get("out_trade_no")
		body, err := common.Marshal(map[string]any{"code": 1, "status": 1, "pid": "1", "out_trade_no": orderNo, "trade_no": "gw-" + orderNo, "type": "alipay", "money": "7.30"})
		assert.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	db := epayServiceTestDB(t, server.URL)
	ctx := context.Background()
	for i := 0; i < 22; i++ {
		order, err := CreateEpayTopUp(ctx, 1, EpayQuoteRequest{Amount: 1, AmountUnit: "USD", PaymentMethod: "alipay"})
		require.NoError(t, err)
		require.NoError(t, db.Model(&model.EpayOrder{}).Where("top_up_id = ?", order.Order.Id).Update("next_check_at", time.Now().Unix()-1).Error)
	}
	setEpayTestOption(t, db, "PayMethods", "[]")
	service := EpayPaymentService{Client: epay.NewClient(server.Client())}
	count, err := service.RecoverOrders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 20, count)
	assert.EqualValues(t, 20, calls.Load())
	assert.LessOrEqual(t, maximum.Load(), int32(2))
	count, err = service.RecoverOrders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 11000000, user.Quota)
}
