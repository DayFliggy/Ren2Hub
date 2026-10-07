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

func epayTestNotification(t *testing.T, order *EpayCreatedOrder, tradeNo, key string) map[string]string {
	t.Helper()
	params := map[string]string{"pid": "1", "out_trade_no": order.Order.TradeNo, "trade_no": tradeNo, "type": "alipay", "money": order.Quote.PayAmount, "trade_status": epay.StatusTradeSuccess, "sign_type": "MD5"}
	sign, err := epay.Sign(params, key)
	require.NoError(t, err)
	params["sign"] = sign
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
	sign, err := epay.Sign(order.Params, "test-only-key")
	require.NoError(t, err)
	assert.Equal(t, order.Params["sign"], sign)
	_, snapshot, merchant, err := model.GetEpayOrder(ctx, order.Order.TradeNo)
	require.NoError(t, err)
	assert.EqualValues(t, 750000, snapshot.ExpectedQuota)
	assert.EqualValues(t, 1095, snapshot.AmountMinor)
	assert.NotEmpty(t, merchant.ID)
	setEpayTestOption(t, db, "EpayKey", "rotated-key")
	setEpayTestOption(t, db, "PayMethods", "[]")
	setEpayTestOption(t, db, "general_setting.quota_display_type", "USD")
	require.Error(t, SettleEpayNotification(ctx, epayTestNotification(t, order, "gateway-1", "rotated-key"), ""))
	callback := epayTestNotification(t, order, "gateway-1", "test-only-key")
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

func TestEpayPricingRejectsInvalidSaveWithoutChangingCheckout(t *testing.T) {
	db := epayServiceTestDB(t, "https://pay.example.test")
	setEpayTestOption(t, db, "TopupGroupRatio", `{"default":1}`)
	request := EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"}
	before, _, err := QuoteEpay(context.Background(), 1, request)
	require.NoError(t, err)
	for _, patch := range []map[string]string{
		{"Price": "0", "MinTopUp": "2"},
		{"Price": "-1"},
		{"Price": "1e2147483647"},
		{"TopupGroupRatio": `{"default":0}`, "MinTopUp": "2"},
		{"TopupGroupRatio": `{"default":-1}`},
	} {
		require.Error(t, model.UpdateOptionsBulk(patch))
		after, _, err := QuoteEpay(context.Background(), 1, request)
		require.NoError(t, err)
		assert.Equal(t, before.QuoteFingerprint, after.QuoteFingerprint)
		assert.Equal(t, "73.00", after.PayAmount)
		assert.EqualValues(t, 1, after.MinTopUp)
	}
}

func TestEpayPricingRejectsInvalidPersistedConfiguration(t *testing.T) {
	for _, option := range []struct{ key, value string }{
		{"Price", "0"},
		{"Price", "1e2147483647"},
		{"TopupGroupRatio", `{"default":-1}`},
	} {
		t.Run(option.key+option.value, func(t *testing.T) {
			db := epayServiceTestDB(t, "https://pay.example.test")
			setEpayTestOption(t, db, option.key, option.value)
			_, err := LoadEpayConfig(context.Background())
			require.Error(t, err)
		})
	}
}

func TestEpaySettlementRejectsNoncanonicalStoredSwitch(t *testing.T) {
	for _, value := range []string{"TRUE", "1", "t", "FALSE", "0", "", "invalid"} {
		t.Run(value, func(t *testing.T) {
			db := epayServiceTestDB(t, "https://pay.example.test")
			ctx := context.Background()
			order, err := CreateEpayTopUp(ctx, 1, EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"})
			require.NoError(t, err)
			setEpayTestOption(t, db, "payment_setting.epay_settlement_enabled", value)
			enabled, err := EpaySettlementEnabled(ctx)
			require.Error(t, err)
			assert.False(t, enabled)
			require.Error(t, SettleEpayNotification(ctx, epayTestNotification(t, order, "invalid-switch", "test-only-key"), ""))
			var user model.User
			require.NoError(t, db.First(&user, 1).Error)
			assert.Zero(t, user.Quota)
			var snapshot model.EpayOrder
			require.NoError(t, db.First(&snapshot, order.Order.Id).Error)
			assert.Nil(t, snapshot.GatewayTradeNo)
			var saved model.TopUp
			require.NoError(t, db.First(&saved, order.Order.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, saved.Status)
			assert.Zero(t, saved.CreditedQuota)
		})
	}
}

func TestEpaySettlementSwitchDefaultsAndPauseResume(t *testing.T) {
	db := epayServiceTestDB(t, "https://pay.example.test")
	ctx := context.Background()
	enabled, err := EpaySettlementEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
	order, err := CreateEpayTopUp(ctx, 1, EpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "alipay"})
	require.NoError(t, err)
	callback := epayTestNotification(t, order, "paused-receipt", "test-only-key")
	setEpayTestOption(t, db, "payment_setting.epay_settlement_enabled", "false")
	enabled, err = EpaySettlementEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
	require.ErrorIs(t, SettleEpayNotification(ctx, callback, ""), model.ErrEpaySettlementPaused)
	setEpayTestOption(t, db, "payment_setting.epay_settlement_enabled", "true")
	require.NoError(t, SettleEpayNotification(ctx, callback, ""))
	require.NoError(t, SettleEpayNotification(ctx, callback, ""))
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(5_000_000), user.Quota)
	var count int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
