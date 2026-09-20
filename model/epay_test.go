package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestEpayMerchantVersionReuseRotationAndReceiptUniqueness(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "epay-versions", AffCode: "epay-versions"}
	require.NoError(t, db.Create(&user).Error)
	orders := make([]TopUp, 3)
	payments := make([]EpayPayment, 3)
	for i, key := range []string{"first-secret", "first-secret", "rotated-secret"} {
		orders[i] = TopUp{UserId: user.Id, TradeNo: fmt.Sprintf("version-%d", i), PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
		payments[i] = insertEpaySnapshotTestOrder(t, &orders[i], 750_000, key)
	}
	assert.Equal(t, payments[0].MerchantVersionID, payments[1].MerchantVersionID)
	assert.NotEqual(t, payments[0].MerchantVersionID, payments[2].MerchantVersionID)
	var versions []EpayMerchantVersion
	require.NoError(t, db.Find(&versions).Error)
	require.Len(t, versions, 2)
	assert.Equal(t, versions[0].AccountID, versions[1].AccountID)
	encoded, err := common.Marshal(versions[0])
	require.NoError(t, err)
	assert.JSONEq(t, "{}", string(encoded))

	_, err = RechargeEpay(payments[0], "")
	require.NoError(t, err)
	for _, index := range []int{1, 2} {
		payments[index].GatewayTradeNo = payments[0].GatewayTradeNo
		_, err = RechargeEpay(payments[index], "")
		require.Error(t, err, "a gateway payment cannot fund another order, including after key rotation")
		var snapshot EpayOrder
		require.NoError(t, db.First(&snapshot, orders[index].Id).Error)
		assert.Nil(t, snapshot.GatewayTradeNo)
		assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(orders[index].TradeNo).Status)
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.Quota(750_000), user.Quota)
}

func TestEpayMerchantFailureDoesNotLogOrReturnSecrets(t *testing.T) {
	db := useWalletIngressDB(t)
	var output bytes.Buffer
	db.Logger = gormlogger.New(log.New(&output, "", 0), gormlogger.Config{LogLevel: gormlogger.Info})
	secret := "private-test-credential"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_epay_merchant", func(tx *gorm.DB) {
		if tx.Statement.Table == "epay_merchant_versions" {
			tx.AddError(errors.New("credential insert failed: " + secret))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("fail_epay_merchant") })
	err := CreateEpayOrder(context.Background(), &TopUp{TradeNo: "private-order", PaymentProvider: PaymentProviderEpay}, &EpayOrder{ExpectedQuota: 1, AmountMinor: 1, Currency: "CNY"}, &EpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant", Key: secret})
	require.ErrorIs(t, err, ErrEpayMerchantUnavailable)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, output.String(), secret)
}

func TestRechargeEpayRejectsMismatchedReceiptBeforeAndAfterSuccess(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "receipt-owner", AffCode: "receipt-owner"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "receipt-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
	payment := insertEpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	for _, success := range []bool{false, true} {
		if success {
			_, err := RechargeEpay(payment, "")
			require.NoError(t, err)
		}
		for _, mutate := range []func(*EpayPayment){
			func(p *EpayPayment) { p.MerchantID = "other-merchant" },
			func(p *EpayPayment) { p.MerchantVersionID = "other-version" },
			func(p *EpayPayment) { p.AmountMinor++ },
			func(p *EpayPayment) { p.GatewayTradeNo = "" },
		} {
			invalid := payment
			mutate(&invalid)
			_, err := RechargeEpay(invalid, "")
			require.ErrorIs(t, err, ErrEpayPaymentMismatch)
		}
	}
	invalid := payment
	invalid.GatewayTradeNo = "different-receipt"
	_, err := RechargeEpay(invalid, "")
	require.ErrorIs(t, err, ErrEpayPaymentMismatch)
	already, err := RechargeEpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.Quota(750_000), user.Quota)
}

func TestEpaySettlementRequiresSnapshot(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "missing-snapshot", AffCode: "missing-snapshot"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "missing-snapshot", Amount: 10, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&order).Error)
	_, err := RechargeEpay(EpayPayment{OrderNo: order.TradeNo}, "")
	require.ErrorIs(t, err, ErrEpaySnapshotMissing)
	require.ErrorIs(t, ManualCompleteTopUp(order.TradeNo, ""), ErrEpaySnapshotMissing)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, order.CreditedQuota)
}

func TestManualCompleteEpayUsesFrozenQuotaAndAcceptsLaterReceipt(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "manual-payer", AffCode: "manual-payer"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "manual-order", Amount: 1, Money: 123, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
	payment := insertEpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.Quota(750_000), order.CreditedQuota)
	var snapshot EpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Nil(t, snapshot.GatewayTradeNo, "forced completion does not require a gateway receipt")
	require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
	already, err := RechargeEpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	already, err = RechargeEpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.Quota(750_000), user.Quota)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, 1, count, "repeated manual completion must not create a false zero-amount top-up log")
}

func TestEpaySettlementPauseRejectsCreditButAllowsForcedCompletion(t *testing.T) {
	for _, setting := range []string{"false", "invalid"} {
		t.Run(setting, func(t *testing.T) {
			db := useWalletIngressDB(t)
			user := User{Username: "paused-payer", AffCode: "paused-payer"}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: "paused-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			payment := insertEpaySnapshotTestOrder(t, &order, 750_000, "test-key")
			require.NoError(t, db.Create(&Option{Key: "payment_setting.epay_settlement_enabled", Value: setting}).Error)
			_, err := RechargeEpay(payment, "")
			require.Error(t, err)
			if setting == "false" {
				assert.ErrorIs(t, err, ErrEpaySettlementPaused)
			}
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			assert.Zero(t, order.CreditedQuota)
			var snapshot EpayOrder
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.Nil(t, snapshot.GatewayTradeNo)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Zero(t, user.Quota)
			var count int64
			require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, common.Quota(750_000), user.Quota)
		})
	}
}

func TestEpayReceiptAndAffiliateRollbackWithFailedCredit(t *testing.T) {
	db := useWalletIngressDB(t)
	enableAffiliatePaymentTest(t)
	inviter := User{Username: "rollback-inviter", AffCode: "rollback-inviter", AffCodeEnabled: true}
	require.NoError(t, db.Create(&inviter).Error)
	user := User{Username: "rollback-payer", AffCode: "rollback-payer", InviterId: inviter.Id}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "rollback-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, CreateTime: time.Now().Unix(), AffiliateBaseQuota: 750_000}
	payment := insertEpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	injected := errors.New("affiliate insert failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_epay_affiliate", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_ledgers" {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("fail_epay_affiliate") })
	_, err := RechargeEpay(payment, "")
	require.ErrorIs(t, err, injected)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, order.CreditedQuota)
	var snapshot EpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Nil(t, snapshot.GatewayTradeNo)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&AffiliateLedger{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestEpayCheckLeaseRecoveryBackoffAndWindow(t *testing.T) {
	db := useWalletIngressDB(t)
	ctx := context.Background()
	now := time.Now().Unix()
	order := TopUp{UserId: 1, TradeNo: "lease-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, CreateTime: now - 120}
	insertEpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	firstToken, err := ClaimEpayCheck(ctx, order.Id, now)
	require.NoError(t, err)
	_, err = ClaimEpayCheck(ctx, order.Id, now+59)
	require.ErrorIs(t, err, ErrEpayCheckThrottled)
	orders, err := PendingEpayChecks(ctx, now+59, 20)
	require.NoError(t, err)
	assert.Empty(t, orders, "expired leases still obey the minimum query interval")
	secondToken, err := ClaimEpayCheck(ctx, order.Id, now+60)
	require.NoError(t, err)
	require.ErrorIs(t, FinishEpayCheck(ctx, order.Id, firstToken, "stale", now+60), gorm.ErrRecordNotFound)
	for i, delay := range []int64{60, 300, 900, 3600} {
		checkedAt := now + 60
		token := secondToken
		if i > 0 {
			var previous EpayOrder
			require.NoError(t, db.First(&previous, order.Id).Error)
			checkedAt = previous.NextCheckAt
			token, err = ClaimEpayCheck(ctx, order.Id, checkedAt)
			require.NoError(t, err)
		}
		require.NoError(t, FinishEpayCheck(ctx, order.Id, token, "network", checkedAt))
		var snapshot EpayOrder
		require.NoError(t, db.First(&snapshot, order.Id).Error)
		assert.Equal(t, checkedAt+delay, snapshot.NextCheckAt)
		assert.Equal(t, "network", snapshot.LastCheckError)
		assert.Empty(t, snapshot.LeaseToken)
		assert.Zero(t, snapshot.LeaseUntil)
	}
	old := TopUp{UserId: 1, TradeNo: "old-pending-order", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, CreateTime: now - 8*24*3600}
	insertEpaySnapshotTestOrder(t, &old, 750_000, "test-key")
	orders, err = PendingEpayChecks(ctx, now, 20)
	require.NoError(t, err)
	assert.Empty(t, orders)
	_, err = ClaimEpayCheck(ctx, old.Id, now)
	require.NoError(t, err, "orders outside the automatic window remain manually checkable")
}

func TestEpayMigrationsAndConcurrentSettlementAcrossDatabases(t *testing.T) {
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(backend.kind), func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " must target an isolated test database")
			}
			db, err := gatewayStartupDatabase(t, backend.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLog := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(backend.kind, backend.kind)
			initCol()
			connection, err := db.DB()
			require.NoError(t, err)
			if backend.kind == common.DatabaseTypeSQLite {
				connection.SetMaxOpenConns(1)
			}
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousKind, previousLogKind)
				initCol()
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &BillingEvent{}, &TopUp{}, &AffiliateLedger{}, &Log{}, &Option{}))
			preserved := TopUp{UserId: 99, TradeNo: "preserved-payment", PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusSuccess, CreditedQuota: 2_000_000}
			require.NoError(t, db.Create(&preserved).Error)
			require.NoError(t, db.AutoMigrate(&EpayOrder{}, &EpayMerchantVersion{}))
			require.NoError(t, db.AutoMigrate(&EpayOrder{}, &EpayMerchantVersion{}))
			require.NoError(t, db.First(&preserved, preserved.Id).Error)
			assert.Equal(t, common.Quota(2_000_000), preserved.CreditedQuota)
			assert.Equal(t, common.TopUpStatusSuccess, preserved.Status)
			enableAffiliatePaymentTest(t)
			inviter := User{Username: "race-inviter", AffCode: "race-inviter", AffCodeEnabled: true}
			require.NoError(t, db.Create(&inviter).Error)
			user := User{Username: "race-payer", AffCode: "race-payer", InviterId: inviter.Id}
			require.NoError(t, db.Create(&user).Error)
			for index, manual := range []bool{false, true} {
				order := TopUp{UserId: user.Id, TradeNo: fmt.Sprintf("race-order-%d", index), Amount: 1, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, CreateTime: time.Now().Unix(), AffiliateBaseQuota: 750_000}
				payment := insertEpaySnapshotTestOrder(t, &order, 750_000, "test-key")
				start := make(chan struct{})
				var wg sync.WaitGroup
				var outcomes [2]error
				wg.Add(2)
				go func() { defer wg.Done(); <-start; _, outcomes[0] = RechargeEpay(payment, "") }()
				go func() {
					defer wg.Done()
					<-start
					if manual {
						outcomes[1] = ManualCompleteTopUp(order.TradeNo, "")
					} else {
						_, outcomes[1] = RechargeEpay(payment, "")
					}
				}()
				close(start)
				wg.Wait()
				for _, outcome := range outcomes {
					require.NoError(t, outcome)
				}
				require.NoError(t, db.First(&order, order.Id).Error)
				assert.Equal(t, common.Quota(750_000), order.CreditedQuota)
				var snapshot EpayOrder
				require.NoError(t, db.First(&snapshot, order.Id).Error)
				require.NotNil(t, snapshot.GatewayTradeNo)
				assert.Equal(t, payment.GatewayTradeNo, *snapshot.GatewayTradeNo)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, common.Quota(1_500_000), user.Quota)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, common.Quota(150_000), inviter.AffQuota)
			var ledgerCount int64
			require.NoError(t, db.Model(&AffiliateLedger{}).Count(&ledgerCount).Error)
			assert.EqualValues(t, 2, ledgerCount)
			duplicate := TopUp{UserId: user.Id, TradeNo: "duplicate-receipt", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			payment := insertEpaySnapshotTestOrder(t, &duplicate, 750_000, "rotated-key")
			payment.GatewayTradeNo = "receipt-race-order-0"
			_, err = RechargeEpay(payment, "")
			require.Error(t, err)
			require.NoError(t, db.First(&duplicate, duplicate.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, duplicate.Status)

			pausedOrder := TopUp{UserId: user.Id, TradeNo: "pause-wins", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			pausedPayment := insertEpaySnapshotTestOrder(t, &pausedOrder, 750_000, "test-key")
			pauseTx := db.Begin()
			require.NoError(t, pauseTx.Error)
			defer pauseTx.Rollback()
			require.NoError(t, pauseTx.Model(&Option{}).Where(&Option{Key: "payment_setting.epay_settlement_enabled"}).Update("value", "false").Error)
			attempted := make(chan struct{}, 1)
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("observe_paused_settlement", func(tx *gorm.DB) {
				if tx.Statement.Table == "options" {
					select {
					case attempted <- struct{}{}:
					default:
					}
				}
			}))
			t.Cleanup(func() { db.Callback().Create().Remove("observe_paused_settlement") })
			settlementResult := make(chan error, 1)
			go func() { _, err := RechargeEpay(pausedPayment, ""); settlementResult <- err }()
			// SQLite's single writer connection itself queues this transaction.
			// Other databases begin concurrently and contend on the option row.
			if backend.kind != common.DatabaseTypeSQLite {
				select {
				case <-attempted:
				case <-time.After(5 * time.Second):
					t.Fatal("settlement did not reach the option lock")
				}
			}
			require.NoError(t, pauseTx.Commit().Error)
			select {
			case err := <-settlementResult:
				require.ErrorIs(t, err, ErrEpaySettlementPaused)
			case <-time.After(5 * time.Second):
				t.Fatal("settlement did not finish after pause committed")
			}
			require.NoError(t, db.First(&pausedOrder, pausedOrder.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, pausedOrder.Status)
			assert.Zero(t, pausedOrder.CreditedQuota)
			var pausedSnapshot EpayOrder
			require.NoError(t, db.First(&pausedSnapshot, pausedOrder.Id).Error)
			assert.Nil(t, pausedSnapshot.GatewayTradeNo)
		})
	}
}
