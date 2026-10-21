package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"log"
	"path/filepath"
	"testing"
)

func useGmpayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldLog := DB, LOG_DB
	oldType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "payment.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &Log{}, &Option{}, &GmpayMerchantVersion{}, &GmpayOrder{}, &AffiliateRecord{}, &AdminQuotaOperation{}))
	DB, LOG_DB = db, db
	common.RedisEnabled = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.SetMainDatabaseType(oldType)
		common.SetLogDatabaseType(oldLogType)
		sqlDB.Close()
	})
	return db
}
func insertGmpaySnapshotTestOrder(t *testing.T, order *TopUp, quota int, key string) GmpayPayment {
	t.Helper()
	snapshot := GmpayOrder{
		AmountMinor: 1000, Currency: "CNY",
		ExpectedQuota: quota,
		Token:         "usdt", Network: "tron",
	}
	merchant := GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant-1", Key: key}
	require.NoError(t, CreateGmpayOrder(context.Background(), order, &snapshot, &merchant))
	return GmpayPayment{
		OrderNo: order.TradeNo, MerchantID: merchant.MerchantID, MerchantVersionID: merchant.ID,
		GatewayTradeNo: "receipt-" + order.TradeNo, AmountMinor: snapshot.AmountMinor,
		Token:        snapshot.Token,
		ActualAmount: "9.99", ReceiveAddress: "final-address", BlockTransactionID: "block-" + order.TradeNo,
	}
}

func TestGmpayMerchantVersionReuseRotationAndReceiptUniqueness(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-versions", AffCode: "gmpay-versions"}
	require.NoError(t, db.Create(&user).Error)
	orders := make([]TopUp, 3)
	payments := make([]GmpayPayment, 3)
	for i, key := range []string{"first-secret", "first-secret", "rotated-secret"} {
		orders[i] = TopUp{UserId: user.Id, TradeNo: fmt.Sprintf("gmpay-version-%d", i), PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
		payments[i] = insertGmpaySnapshotTestOrder(t, &orders[i], 750_000, key)
	}
	assert.Equal(t, payments[0].MerchantVersionID, payments[1].MerchantVersionID)
	assert.NotEqual(t, payments[0].MerchantVersionID, payments[2].MerchantVersionID)
	var versions []GmpayMerchantVersion
	require.NoError(t, db.Find(&versions).Error)
	require.Len(t, versions, 2)
	assert.Equal(t, versions[0].AccountID, versions[1].AccountID)
	encoded, err := common.Marshal(versions[0])
	require.NoError(t, err)
	assert.JSONEq(t, "{}", string(encoded))

	_, err = RechargeGmpay(payments[0], "")
	require.NoError(t, err)
	for _, index := range []int{1, 2} {
		payments[index].GatewayTradeNo = payments[0].GatewayTradeNo
		_, err = RechargeGmpay(payments[index], "")
		require.Error(t, err, "one gateway trade cannot fund another order, including after key rotation")
		var snapshot GmpayOrder
		require.NoError(t, db.First(&snapshot, orders[index].Id).Error)
		assert.Nil(t, snapshot.GatewayTradeNo)
		assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(orders[index].TradeNo).Status)
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, int(750_000), user.Quota)
}

func TestGmpayMerchantFailureDoesNotLogOrReturnSecrets(t *testing.T) {
	db := useGmpayTestDB(t)
	var output bytes.Buffer
	db.Logger = gormlogger.New(log.New(&output, "", 0), gormlogger.Config{LogLevel: gormlogger.Info})
	secret := "private-test-credential"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_gmpay_merchant", func(tx *gorm.DB) {
		if tx.Statement.Table == "gmpay_merchant_versions" {
			tx.AddError(errors.New("credential insert failed: " + secret))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("fail_gmpay_merchant") })
	err := CreateGmpayOrder(
		context.Background(),
		&TopUp{TradeNo: "gmpay-private-order", PaymentProvider: PaymentProviderGmpay},
		&GmpayOrder{ExpectedQuota: 1, AmountMinor: 1, Currency: "CNY", Token: "usdt", Network: "tron"},
		&GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant", Key: secret},
	)
	require.ErrorIs(t, err, ErrGmpayMerchantUnavailable)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, output.String(), secret)
}

func TestRechargeGmpayRejectsMismatchedReceiptBeforeAndAfterSuccess(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-receipt-owner", AffCode: "gmpay-receipt-owner"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-receipt-order", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	for _, success := range []bool{false, true} {
		if success {
			_, err := RechargeGmpay(payment, "")
			require.NoError(t, err)
		}
		// The fiat amount is checked by the shared receipt check in settleTopUp.
		amountChanged := payment
		amountChanged.AmountMinor++
		_, err := RechargeGmpay(amountChanged, "")
		require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
		for _, mutate := range []func(*GmpayPayment){
			func(p *GmpayPayment) { p.MerchantID = "other-merchant" },
			func(p *GmpayPayment) { p.MerchantVersionID = "other-version" },
			func(p *GmpayPayment) { p.GatewayTradeNo = "" },
			func(p *GmpayPayment) { p.Token = "" },
			func(p *GmpayPayment) { p.Token = "usdc" },
			func(p *GmpayPayment) { p.ActualAmount = "" },
			func(p *GmpayPayment) { p.ActualAmount = "0" },
			func(p *GmpayPayment) { p.ReceiveAddress = "" },
			func(p *GmpayPayment) { p.BlockTransactionID = "" },
		} {
			invalid := payment
			mutate(&invalid)
			_, err := RechargeGmpay(invalid, "")
			require.ErrorIs(t, err, ErrGmpayPaymentMismatch)
		}
	}
	invalid := payment
	invalid.GatewayTradeNo = "different-receipt"
	_, err := RechargeGmpay(invalid, "")
	require.ErrorIs(t, err, ErrGmpayPaymentMismatch)
	already, err := RechargeGmpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, int(750_000), user.Quota)
}

func TestGmpaySettlementRequiresSnapshot(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-missing-snapshot", AffCode: "gmpay-missing-snapshot"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-missing-snapshot", Amount: 10, PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&order).Error)
	_, err := RechargeGmpay(GmpayPayment{OrderNo: order.TradeNo}, "")
	require.ErrorIs(t, err, ErrGmpaySnapshotMissing)
	require.ErrorIs(t, ManualCompleteTopUp(order.TradeNo, ""), ErrGmpaySnapshotMissing)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)

}

func TestCreateGmpayOrderRejectsIncompleteSnapshot(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-snapshot-metadata", AffCode: "gmpay-snapshot-metadata"}
	require.NoError(t, db.Create(&user).Error)
	base := GmpayOrder{
		AmountMinor: 1000, Currency: "CNY",
		ExpectedQuota: 750_000,
		Token:         "usdt", Network: "tron",
	}
	merchant := GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant-1", Key: "test-key"}
	for _, test := range []struct {
		name   string
		mutate func(*GmpayOrder)
	}{
		{"missing chain", func(s *GmpayOrder) { s.Network = "" }},
		{"missing token", func(s *GmpayOrder) { s.Token = "" }},
		{"wrong currency", func(s *GmpayOrder) { s.Currency = "USD" }},
		{"zero frozen quota", func(s *GmpayOrder) { s.ExpectedQuota = 0 }},
		{"zero payable amount", func(s *GmpayOrder) { s.AmountMinor = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			order := TopUp{UserId: user.Id, TradeNo: "gmpay-snapshot-" + test.name, PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			snapshot := base
			test.mutate(&snapshot)
			candidate := merchant
			require.ErrorIs(t, CreateGmpayOrder(context.Background(), &order, &snapshot, &candidate), ErrGmpayPaymentMismatch)
			var count int64
			require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", order.TradeNo).Count(&count).Error)
			assert.Zero(t, count, "a rejected snapshot must not leave a local order behind")
		})
	}
}

func TestManualCompleteGmpayUsesFrozenQuotaAndAcceptsLaterReceipt(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-manual-payer", AffCode: "gmpay-manual-payer"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-manual-order", Amount: 1, Money: 1, PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
	require.NoError(t, db.First(&order, order.Id).Error)

	var snapshot GmpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Nil(t, snapshot.GatewayTradeNo, "forced completion does not require a gateway receipt")
	assert.Equal(t, GmpayStatusSettled, snapshot.Status)
	assert.Empty(t, snapshot.SettledCryptoAmount)
	assert.Empty(t, snapshot.SettledReceiveAddress)
	assert.Empty(t, snapshot.SettledTransactionID)
	assert.Equal(t, "usdt", snapshot.Token)
	assert.Equal(t, "tron", snapshot.Network)
	require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
	already, err := RechargeGmpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	already, err = RechargeGmpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Equal(t, payment.ActualAmount, snapshot.SettledCryptoAmount)
	assert.Equal(t, payment.ReceiveAddress, snapshot.SettledReceiveAddress)
	assert.Equal(t, payment.BlockTransactionID, snapshot.SettledTransactionID)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, int(750_000), user.Quota)
	var count int64
	require.NoError(t, db.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, 1, count, "repeated manual completion must not create a false zero-amount top-up log")
}

func TestGmpayCreateResponseBindsReceiptBeforeSettlement(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-create-receipt", AffCode: "gmpay-create-receipt"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-create-receipt", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	result := GmpayOrderResult{
		Status: GmpayStatusReady, GatewayTradeNo: payment.GatewayTradeNo, GatewayAmount: "10.00",
		GatewayCryptoAmount: "8.75", ReceiveAddress: "test-address", ExpirationTime: 1893456000,
	}
	require.NoError(t, MarkGmpayOrderResult(context.Background(), order.Id, result))
	var snapshot GmpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	require.NotNil(t, snapshot.GatewayTradeNo)
	assert.Equal(t, payment.GatewayTradeNo, *snapshot.GatewayTradeNo)
	assert.Equal(t, GmpayStatusReady, snapshot.Status)
	assert.Equal(t, result.GatewayCryptoAmount, snapshot.GatewayCryptoAmt)

	invalid := payment
	invalid.GatewayTradeNo = "unrelated-receipt"
	_, err := RechargeGmpay(invalid, "")
	require.ErrorIs(t, err, ErrGmpayPaymentMismatch)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)

	duplicate := TopUp{UserId: user.Id, TradeNo: "gmpay-duplicate-create", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	insertGmpaySnapshotTestOrder(t, &duplicate, 750_000, "rotated-key")
	require.Error(t, MarkGmpayOrderResult(context.Background(), duplicate.Id, result))
	var duplicateSnapshot GmpayOrder
	require.NoError(t, db.First(&duplicateSnapshot, duplicate.Id).Error)
	assert.Nil(t, duplicateSnapshot.GatewayTradeNo)
	assert.Equal(t, GmpayStatusCreating, duplicateSnapshot.Status)

	_, err = RechargeGmpay(payment, "")
	require.NoError(t, err)
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Equal(t, GmpayStatusSettled, snapshot.Status)
	assert.Equal(t, result.GatewayCryptoAmount, snapshot.GatewayCryptoAmt)
	assert.Equal(t, result.ReceiveAddress, snapshot.ReceiveAddress)
	assert.Equal(t, payment.ActualAmount, snapshot.SettledCryptoAmount)
	assert.Equal(t, payment.ReceiveAddress, snapshot.SettledReceiveAddress)
	assert.Equal(t, payment.BlockTransactionID, snapshot.SettledTransactionID)
	for _, mutate := range []func(*GmpayPayment){
		func(p *GmpayPayment) { p.ActualAmount = "10.01" },
		func(p *GmpayPayment) { p.ReceiveAddress = "other-address" },
		func(p *GmpayPayment) { p.BlockTransactionID = "other-transaction" },
	} {
		mismatched := payment
		mutate(&mismatched)
		_, err = RechargeGmpay(mismatched, "")
		require.ErrorIs(t, err, ErrGmpayPaymentMismatch)
	}
	// Equivalent decimal spelling does not change the verified payment.
	payment.ActualAmount = "9.990000"
	already, err := RechargeGmpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, int(750_000), user.Quota)
}

func TestGmpayLateCreateResultPreservesCompletedOrder(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual=%t", manual), func(t *testing.T) {
			db := useGmpayTestDB(t)
			user := User{Username: "gmpay-early-settlement", AffCode: "gmpay-early-settlement"}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: "gmpay-early-settlement", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
			if manual {
				require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
			} else {
				_, err := RechargeGmpay(payment, "")
				require.NoError(t, err)
			}
			result := GmpayOrderResult{
				Status: GmpayStatusReady, GatewayTradeNo: payment.GatewayTradeNo, GatewayAmount: "10.00",
				GatewayCryptoAmount: "9.99", ReceiveAddress: "test-address", ExpirationTime: 1893456000,
			}
			if !manual {
				mismatch := result
				mismatch.GatewayTradeNo = "unrelated-receipt"
				require.ErrorIs(t, MarkGmpayOrderResult(context.Background(), order.Id, mismatch), ErrGmpayPaymentMismatch)
			}
			require.NoError(t, MarkGmpayOrderResult(context.Background(), order.Id, result))
			require.NoError(t, MarkGmpayOrderResult(context.Background(), order.Id, result))
			changed := result
			changed.GatewayCryptoAmount = "12.34"
			require.ErrorIs(t, MarkGmpayOrderResult(context.Background(), order.Id, changed), ErrGmpayStateConflict)
			for _, status := range []string{GmpayStatusFailed, GmpayStatusUnknown} {
				require.NoError(t, MarkGmpayOrderResult(context.Background(), order.Id, GmpayOrderResult{Status: status, Failure: "TRANSPORT"}))
			}
			var snapshot GmpayOrder
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.Equal(t, GmpayStatusSettled, snapshot.Status)
			require.NotNil(t, snapshot.GatewayTradeNo)
			assert.Equal(t, payment.GatewayTradeNo, *snapshot.GatewayTradeNo)
			assert.Equal(t, result.GatewayAmount, snapshot.GatewayAmount)
			assert.Equal(t, result.GatewayCryptoAmount, snapshot.GatewayCryptoAmt)
			assert.Empty(t, snapshot.LastFailure)
			already, err := RechargeGmpay(payment, "")
			require.NoError(t, err)
			assert.True(t, already)
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.Equal(t, payment.ActualAmount, snapshot.SettledCryptoAmount)
			assert.Equal(t, payment.ReceiveAddress, snapshot.SettledReceiveAddress)
			assert.Equal(t, payment.BlockTransactionID, snapshot.SettledTransactionID)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, int(750_000), user.Quota)

		})
	}
}

func TestGmpayVerifiedPaymentSettlesFailedAndUnknownCreations(t *testing.T) {
	for _, status := range []string{GmpayStatusFailed, GmpayStatusUnknown} {
		t.Run(status, func(t *testing.T) {
			db := useGmpayTestDB(t)
			user := User{Username: "gmpay-late-proof", AffCode: "gmpay-late-proof"}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: "gmpay-late-proof", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
			require.NoError(t, MarkGmpayOrderResult(context.Background(), order.Id, GmpayOrderResult{Status: status, Failure: "TRANSPORT"}))
			// A local creation outcome is not authority to claim payment; only
			// a verified callback can move this order into the settled state.
			result := GmpayOrderResult{Status: GmpayStatusReady, GatewayTradeNo: payment.GatewayTradeNo, GatewayAmount: "10.00", GatewayCryptoAmount: "9.99", ReceiveAddress: "initial-address", ExpirationTime: 1893456000}
			require.ErrorIs(t, MarkGmpayOrderResult(context.Background(), order.Id, result), ErrGmpayStateConflict)
			already, err := RechargeGmpay(payment, "")
			require.NoError(t, err)
			assert.False(t, already)
			require.NoError(t, MarkGmpayOrderResult(context.Background(), order.Id, result))
			var snapshot GmpayOrder
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.Equal(t, GmpayStatusSettled, snapshot.Status)
			assert.Equal(t, result.ReceiveAddress, snapshot.ReceiveAddress)
			assert.Equal(t, payment.ReceiveAddress, snapshot.SettledReceiveAddress)
			already, err = RechargeGmpay(payment, "")
			require.NoError(t, err)
			assert.True(t, already)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, int(750_000), user.Quota)
		})
	}
}

func TestRechargeGmpayKeepsOrderPendingWhenWalletCannotHoldQuota(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-full-wallet", AffCode: "gmpay-full-wallet", Quota: common.MaxWalletQuota}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-wallet-limit", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 500_000, "test-key")
	_, err := RechargeGmpay(payment, "")
	require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)

	var snapshot GmpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Nil(t, snapshot.GatewayTradeNo, "an unsettled wallet limit must not consume the receipt")
	assert.Empty(t, snapshot.SettledCryptoAmount)
	assert.Empty(t, snapshot.SettledReceiveAddress)
	assert.Empty(t, snapshot.SettledTransactionID)

}

func TestGmpayNonPendingOrderDoesNotConsumePaymentEvidence(t *testing.T) {
	for _, status := range []string{common.TopUpStatusFailed, common.TopUpStatusExpired} {
		t.Run(status, func(t *testing.T) {
			db := useGmpayTestDB(t)
			user := User{Username: "gmpay-inactive", AffCode: "gmpay-inactive"}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: "gmpay-inactive", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
			require.NoError(t, UpdatePendingTopUpStatus(order.TradeNo, PaymentProviderGmpay, status))
			_, err := RechargeGmpay(payment, "")
			require.ErrorIs(t, err, ErrTopUpStatusInvalid)
			require.Error(t, ManualCompleteTopUp(order.TradeNo, ""))
			var snapshot GmpayOrder
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.Equal(t, GmpayStatusCreating, snapshot.Status)
			assert.Nil(t, snapshot.GatewayTradeNo)
			assert.Empty(t, snapshot.SettledCryptoAmount)
			assert.Empty(t, snapshot.SettledReceiveAddress)
			assert.Empty(t, snapshot.SettledTransactionID)
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, status, order.Status)

			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Zero(t, user.Quota)

		})
	}
}
