package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const gmpayTestSecret = "test-only-key"

// gmpayServiceTestDB mirrors epayServiceTestDB: an in-memory order store plus the
// public configuration a callback is verified against.
func gmpayServiceTestDB(t *testing.T, gateway string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.TopUp{}, &model.GmpayOrder{}, &model.GmpayMerchantVersion{}, &model.BillingEvent{}, &model.Log{}))
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
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "gmpay-buyer", Group: "default", AffCode: "gmpay-buyer"}).Error)
	options := map[string]string{
		"GmpayPayAddress": gateway, "GmpayId": "1", "GmpayKey": gmpayTestSecret,
		"GmpayPrice": "1", "GmpayMinTopUp": "1", "GmpayGroupRatio": `{"default":1}`,
		"GmpayAssets":                              `[{"name":"USDT-TRON","token":"usdt","network":"tron"}]`,
		"ServerAddress":                            "https://wallet.example.test",
		"payment_setting.compliance_confirmed":     "true",
		"payment_setting.compliance_terms_version": "v1",
	}
	for key, value := range options {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	return db
}

func setGmpayTestOption(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	require.NoError(t, db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&model.Option{Key: key, Value: value}).Error)
}

// gmpayTestSnapshot stores one frozen order exactly as creation does, so
// settlement always runs against a snapshot rather than a bare order row.
func gmpayTestSnapshot(t *testing.T, order *model.TopUp, quota common.Quota) *model.GmpayOrder {
	t.Helper()
	order.Status = common.TopUpStatusPending
	order.Amount, order.Money = 10, 10
	order.PaymentProvider = model.PaymentProviderGmpay
	if order.PaymentMethod == "" {
		order.PaymentMethod = "gmpay:usdt.tron"
	}
	snapshot := &model.GmpayOrder{
		InputAmount: 10, AmountUnit: "USD", AmountMinor: 1000, Currency: "USD",
		ExpectedQuota: quota, UnitPrice: "1", GroupRatio: "1", Discount: "1",
		Token: "usdt", Network: "tron", QuoteFingerprint: "test-quote",
	}
	merchant := &model.GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "1", Key: gmpayTestSecret}
	require.NoError(t, model.CreateGmpayOrder(context.Background(), order, snapshot, merchant))
	return snapshot
}

// gmpayTestNotification signs a well-formed success callback, matching the flat
// field names the gateway posts.
func gmpayTestNotification(order *model.TopUp, tradeNo, key string, overrides map[string]string) map[string]string {
	params := map[string]string{
		"pid": "1", "trade_id": tradeNo, "order_id": order.TradeNo, "token": "usdt",
		"block_transaction_id": "0xdeadbeef", "amount": "10.00", "actual_amount": "9.998765",
		"receive_address": "TXtestReceiveAddress",
		"status":          "2",
	}
	for name, value := range overrides {
		params[name] = value
	}
	signature, err := gmpay.Sign(params, key)
	if err != nil {
		panic(err)
	}
	params["signature"] = signature
	return params
}

func gmpayTestUserQuota(t *testing.T, db *gorm.DB) common.Quota {
	t.Helper()
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	return user.Quota
}

// TestGmpayOrderCreationBindsTheConfirmedQuote proves a confirmation is only
// accepted while the quote it was shown with still holds, and that a rejected
// confirmation leaves nothing behind.
func TestGmpayOrderCreationBindsTheConfirmedQuote(t *testing.T) {
	db := gmpayServiceTestDB(t, "https://payments.example.test")
	ctx := context.Background()
	request := GmpayQuoteRequest{Amount: 10, AmountUnit: "USD", PaymentMethod: "gmpay:usdt.tron"}
	quote, _, err := QuoteGmpay(ctx, 1, request)
	require.NoError(t, err)
	assert.Equal(t, "10.00", quote.PayAmount)
	assert.Equal(t, common.Quota(5_000_000), quote.ExpectedQuota)

	request.QuoteFingerprint = quote.QuoteFingerprint
	setGmpayTestOption(t, db, "GmpayPrice", "2")
	_, err = CreateGmpayTopUp(ctx, 1, request)
	var changed *GmpayError
	require.ErrorAs(t, err, &changed)
	assert.Equal(t, "QUOTE_CHANGED", changed.Code)
	var count int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&count).Error)
	assert.Zero(t, count, "a rejected confirmation must not persist an order")
}

// TestGetGmpayOrderRejectsForeignAndIncompleteOrders keeps the callback path
// from settling anything but a complete crypto order.
func TestGetGmpayOrderRejectsForeignAndIncompleteOrders(t *testing.T) {
	db := gmpayServiceTestDB(t, "https://payments.example.test")
	ctx := context.Background()
	_, _, _, err := model.GetGmpayOrder(ctx, "GPmissing0000000000000000000000")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	foreign := &model.TopUp{
		UserId: 1, TradeNo: "EPforeign0000000000000000000000", Amount: 1, Money: 7.3,
		PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay,
		Status: common.TopUpStatusPending, CreateTime: 0,
	}
	require.NoError(t, db.Create(foreign).Error)
	_, _, _, err = model.GetGmpayOrder(ctx, foreign.TradeNo)
	require.ErrorIs(t, err, model.ErrPaymentMethodMismatch)

	bare := &model.TopUp{
		UserId: 1, TradeNo: "GPbare000000000000000000000000", Amount: 1, Money: 1,
		PaymentMethod: "gmpay:usdt.tron", PaymentProvider: model.PaymentProviderGmpay,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(bare).Error)
	_, _, _, err = model.GetGmpayOrder(ctx, bare.TradeNo)
	require.ErrorIs(t, err, model.ErrGmpaySnapshotMissing)
}

// TestGmpaySettlementRejectsEveryUnprovenCallback is the core safety table: the
// callback carries no network and no currency, so only the frozen snapshot can
// authorize a credit, and every disagreement leaves the order pending.
func TestGmpaySettlementRejectsEveryUnprovenCallback(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		overrides map[string]string
		key       string
		code      string
		credited  bool
	}{
		{name: "signed success settles", key: gmpayTestSecret, credited: true},
		{name: "foreign signature is rejected", key: "rotated-elsewhere", code: "INVALID_SIGNATURE"},
		{name: "fiat amount must match the snapshot", key: gmpayTestSecret, code: "PAYMENT_MISMATCH",
			overrides: map[string]string{"amount": "9.99"}},
		{name: "another token is rejected", key: gmpayTestSecret, code: "PAYMENT_MISMATCH",
			overrides: map[string]string{"token": "usdc"}},
		{name: "another merchant id is rejected", key: gmpayTestSecret, code: "INVALID_PARAMETERS",
			overrides: map[string]string{"pid": "2"}},
		{name: "a missing chain transaction is rejected", key: gmpayTestSecret, code: "INVALID_PARAMETERS",
			overrides: map[string]string{"block_transaction_id": ""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// One order store per case: a credited balance from one row must not
			// make a later row's "nothing was credited" assertion meaningless.
			db := gmpayServiceTestDB(t, "https://payments.example.test")
			order := &model.TopUp{UserId: 1, TradeNo: gmpayTestTradeNoString(1)}
			gmpayTestSnapshot(t, order, 5_000_000)
			params := gmpayTestNotification(order, "trade-"+order.TradeNo, test.key, test.overrides)
			err := SettleGmpayNotification(ctx, params, "203.0.113.10")
			if test.code == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, test.code, GmpayFailureCode(err))
			}
			require.NoError(t, db.First(&order, order.Id).Error)
			if test.credited {
				assert.Equal(t, common.TopUpStatusSuccess, order.Status)
				assert.Equal(t, common.Quota(5_000_000), order.CreditedQuota)
				assert.Equal(t, common.Quota(5_000_000), gmpayTestUserQuota(t, db))
			} else {
				assert.Equal(t, common.TopUpStatusPending, order.Status)
				assert.Zero(t, order.CreditedQuota)
				assert.Zero(t, gmpayTestUserQuota(t, db), "an unproven callback must never credit quota")
			}
		})
	}
}

// A signed waiting or expired status does not acknowledge payment completion.
func TestGmpaySettlementRejectsUnsettleableNotifications(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		overrides map[string]string
	}{
		{name: "waiting for payment", overrides: map[string]string{"status": "1"}},
		{name: "expired", overrides: map[string]string{"status": "3"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := gmpayServiceTestDB(t, "https://payments.example.test")
			order := &model.TopUp{UserId: 1, TradeNo: gmpayTestTradeNoString(2)}
			snapshot := gmpayTestSnapshot(t, order, 5_000_000)
			params := gmpayTestNotification(order, "trade-"+order.TradeNo, gmpayTestSecret, test.overrides)
			require.ErrorIs(t, SettleGmpayNotification(ctx, params, ""), gmpay.ErrInvalidParameters)
			var saved model.GmpayOrder
			require.NoError(t, db.First(&saved, order.Id).Error)
			assert.Equal(t, "usdt", saved.Token)
			assert.Equal(t, "tron", saved.Network)
			assert.Equal(t, snapshot.ExpectedQuota, saved.ExpectedQuota)
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			assert.Zero(t, gmpayTestUserQuota(t, db))
		})
	}
}

// TestGmpaySettlementIsIdempotent keeps a retried notification, which the
// gateway is expected to send, from crediting twice.
func TestGmpaySettlementIsIdempotent(t *testing.T) {
	db := gmpayServiceTestDB(t, "https://payments.example.test")
	ctx := context.Background()
	order := &model.TopUp{UserId: 1, TradeNo: gmpayTestTradeNoString(3)}
	gmpayTestSnapshot(t, order, 5_000_000)
	params := gmpayTestNotification(order, "trade-retry", gmpayTestSecret, nil)
	require.NoError(t, SettleGmpayNotification(ctx, params, ""))
	require.NoError(t, SettleGmpayNotification(ctx, params, ""))
	assert.Equal(t, common.Quota(5_000_000), gmpayTestUserQuota(t, db))
	var events int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&events).Error)
	assert.Equal(t, int64(1), events)
}

// TestGmpaySettlementHonoursThePauseSwitch proves the administrator switch is
// consulted before any credit and that flipping it stops a later callback.
func TestGmpaySettlementHonoursThePauseSwitch(t *testing.T) {
	db := gmpayServiceTestDB(t, "https://payments.example.test")
	ctx := context.Background()
	order := &model.TopUp{UserId: 1, TradeNo: gmpayTestTradeNoString(4)}
	gmpayTestSnapshot(t, order, 5_000_000)
	setGmpayTestOption(t, db, "payment_setting.gmpay_settlement_enabled", "false")
	err := SettleGmpayNotification(ctx, gmpayTestNotification(order, "trade-paused", gmpayTestSecret, nil), "")
	require.ErrorIs(t, err, model.ErrGmpaySettlementPaused)
	assert.Zero(t, gmpayTestUserQuota(t, db))

	enabled, err := GmpaySettlementEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
	setGmpayTestOption(t, db, "payment_setting.gmpay_settlement_enabled", "true")
	require.NoError(t, SettleGmpayNotification(ctx, gmpayTestNotification(order, "trade-resumed", gmpayTestSecret, nil), ""))
	assert.Equal(t, common.Quota(5_000_000), gmpayTestUserQuota(t, db))
}

// TestGmpaySettlementFailureCodesAreBounded keeps operator-facing categories
// stable and free of credentials or raw upstream errors.
func TestGmpaySettlementFailureCodesAreBounded(t *testing.T) {
	assert.Equal(t, "", GmpayFailureCode(nil))
	assert.Equal(t, "SETTLEMENT_PAUSED", GmpayFailureCode(model.ErrGmpaySettlementPaused))
	assert.Equal(t, "PAYMENT_MISMATCH", GmpayFailureCode(model.ErrGmpayPaymentMismatch))
	assert.Equal(t, "SNAPSHOT_MISSING", GmpayFailureCode(model.ErrGmpaySnapshotMissing))
	assert.Equal(t, "ORDER_NOT_FOUND", GmpayFailureCode(model.ErrTopUpNotFound))
	assert.Equal(t, "INVALID_SIGNATURE", GmpayFailureCode(gmpay.ErrInvalidSignature))
	assert.Equal(t, "INVALID_PARAMETERS", GmpayFailureCode(gmpay.ErrInvalidParameters))
	assert.Equal(t, "SETTLEMENT_FAILED", GmpayFailureCode(gorm.ErrInvalidDB))
	assert.NotContains(t, GmpayFailureCode(gorm.ErrInvalidDB), gmpayTestSecret)
}

// gmpayTestTradeNoString builds a distinct, gateway-legal order number per
// suite so a case can never settle an order another case already settled.
func gmpayTestTradeNoString(suite int) string {
	return "GPt" + string(rune('a'+suite)) + "0000000000000000000000000000"
}
