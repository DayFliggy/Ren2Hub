package service

import (
	"context"
	"testing"

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
