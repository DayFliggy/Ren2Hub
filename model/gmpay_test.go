package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"math"
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

func insertGmpaySnapshotTestOrder(t *testing.T, order *TopUp, quota common.Quota, key string) GmpayPayment {
	t.Helper()
	snapshot := GmpayOrder{
		InputAmount: int64(quota), AmountUnit: "TOKENS", AmountMinor: 1000, Currency: "USD",
		ExpectedQuota: quota, UnitPrice: "1", GroupRatio: "1", Discount: "1",
		QuoteFingerprint: "test-quote", Token: "usdt", Network: "tron",
	}
	merchant := GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant-1", Key: key}
	require.NoError(t, CreateGmpayOrder(context.Background(), order, &snapshot, &merchant))
	return GmpayPayment{
		OrderNo: order.TradeNo, MerchantID: merchant.MerchantID, MerchantVersionID: merchant.ID,
		GatewayTradeNo: "receipt-" + order.TradeNo, AmountMinor: snapshot.AmountMinor,
		Token: snapshot.Token, PaymentMethod: order.PaymentMethod,
		ActualAmount: "9.99", ReceiveAddress: "final-address", BlockTransactionID: "block-" + order.TradeNo,
	}
}

func gmpayPaymentForTest(t *testing.T, order TopUp, method string) GmpayPayment {
	t.Helper()
	payment := GmpayPayment{OrderNo: order.TradeNo, PaymentMethod: method}
	if order.PaymentProvider != PaymentProviderGmpay {
		return payment
	}
	_, snapshot, merchant, err := GetGmpayOrder(context.Background(), order.TradeNo)
	require.NoError(t, err)
	payment.MerchantID, payment.MerchantVersionID = merchant.MerchantID, merchant.ID
	payment.GatewayTradeNo, payment.AmountMinor, payment.Token = "receipt-"+order.TradeNo, snapshot.AmountMinor, snapshot.Token
	payment.ActualAmount, payment.ReceiveAddress, payment.BlockTransactionID = "9.99", "final-address", "block-"+order.TradeNo
	return payment
}

func TestGmpayMerchantVersionReuseRotationAndReceiptUniqueness(t *testing.T) {
	db := useWalletIngressDB(t)
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
	assert.Equal(t, common.Quota(750_000), user.Quota)
}

func TestGmpayMerchantFailureDoesNotLogOrReturnSecrets(t *testing.T) {
	db := useWalletIngressDB(t)
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
		&GmpayOrder{ExpectedQuota: 1, AmountMinor: 1, Currency: "USD", Token: "usdt", Network: "tron"},
		&GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant", Key: secret},
	)
	require.ErrorIs(t, err, ErrGmpayMerchantUnavailable)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, output.String(), secret)
}

func TestRechargeGmpayRejectsMismatchedReceiptBeforeAndAfterSuccess(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "gmpay-receipt-owner", AffCode: "gmpay-receipt-owner"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-receipt-order", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	for _, success := range []bool{false, true} {
		if success {
			_, err := RechargeGmpay(payment, "")
			require.NoError(t, err)
		}
		for _, mutate := range []func(*GmpayPayment){
			func(p *GmpayPayment) { p.MerchantID = "other-merchant" },
			func(p *GmpayPayment) { p.MerchantVersionID = "other-version" },
			func(p *GmpayPayment) { p.AmountMinor++ },
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
	assert.Equal(t, common.Quota(750_000), user.Quota)
}

func TestGmpaySettlementRequiresSnapshot(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "gmpay-missing-snapshot", AffCode: "gmpay-missing-snapshot"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-missing-snapshot", Amount: 10, PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&order).Error)
	_, err := RechargeGmpay(GmpayPayment{OrderNo: order.TradeNo}, "")
	require.ErrorIs(t, err, ErrGmpaySnapshotMissing)
	require.ErrorIs(t, ManualCompleteTopUp(order.TradeNo, ""), ErrGmpaySnapshotMissing)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, order.CreditedQuota)
}

func TestCreateGmpayOrderRejectsIncompleteSnapshot(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "gmpay-snapshot-metadata", AffCode: "gmpay-snapshot-metadata"}
	require.NoError(t, db.Create(&user).Error)
	base := GmpayOrder{
		InputAmount: 750_000, AmountUnit: "TOKENS", AmountMinor: 1000, Currency: "USD",
		ExpectedQuota: 750_000, UnitPrice: "1", GroupRatio: "1", Discount: "1",
		QuoteFingerprint: "test-quote", Token: "usdt", Network: "tron",
	}
	merchant := GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant-1", Key: "test-key"}
	for _, test := range []struct {
		name   string
		mutate func(*GmpayOrder)
	}{
		{"missing chain", func(s *GmpayOrder) { s.Network = "" }},
		{"missing token", func(s *GmpayOrder) { s.Token = "" }},
		{"non-USD currency", func(s *GmpayOrder) { s.Currency = "CNY" }},
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
	db := useWalletIngressDB(t)
	user := User{Username: "gmpay-manual-payer", AffCode: "gmpay-manual-payer"}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-manual-order", Amount: 1, Money: 1, PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.Quota(750_000), order.CreditedQuota)
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
	assert.Equal(t, common.Quota(750_000), user.Quota)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, 1, count, "repeated manual completion must not create a false zero-amount top-up log")
}

func TestGmpayCreateResponseBindsReceiptBeforeSettlement(t *testing.T) {
	db := useWalletIngressDB(t)
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
	assert.Equal(t, common.Quota(750_000), user.Quota)
}

func TestGmpayLateCreateResultPreservesCompletedOrder(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual=%t", manual), func(t *testing.T) {
			db := useWalletIngressDB(t)
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
			assert.Equal(t, common.Quota(750_000), user.Quota)
			var events int64
			require.NoError(t, db.Model(&BillingEvent{}).Count(&events).Error)
			assert.EqualValues(t, 1, events)
		})
	}
}

func TestGmpaySettlementPauseRejectsCreditButAllowsForcedCompletion(t *testing.T) {
	for _, setting := range []string{"false", "invalid"} {
		t.Run(setting, func(t *testing.T) {
			db := useWalletIngressDB(t)
			user := User{Username: "gmpay-paused-payer", AffCode: "gmpay-paused-payer"}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: "gmpay-paused-order", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
			require.NoError(t, db.Create(&Option{Key: "payment_setting.gmpay_settlement_enabled", Value: setting}).Error)
			_, err := RechargeGmpay(payment, "")
			require.Error(t, err)
			if setting == "false" {
				assert.ErrorIs(t, err, ErrGmpaySettlementPaused)
			}
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			assert.Zero(t, order.CreditedQuota)
			var snapshot GmpayOrder
			require.NoError(t, db.First(&snapshot, order.Id).Error)
			assert.Nil(t, snapshot.GatewayTradeNo, "a paused settlement must not record the gateway receipt")
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

func TestRechargeGmpayKeepsOrderPendingWhenWalletCannotHoldQuota(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "gmpay-full-wallet", AffCode: "gmpay-full-wallet", Quota: math.MaxInt64}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-wallet-limit", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	payment := insertGmpaySnapshotTestOrder(t, &order, 500_000, "test-key")
	_, err := RechargeGmpay(payment, "")
	require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, order.CreditedQuota)
	var snapshot GmpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Nil(t, snapshot.GatewayTradeNo, "an unsettled wallet limit must not consume the receipt")
	assert.Empty(t, snapshot.SettledCryptoAmount)
	assert.Empty(t, snapshot.SettledReceiveAddress)
	assert.Empty(t, snapshot.SettledTransactionID)
	var events int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&events).Error)
	assert.Zero(t, events)
}

func TestGmpayReceiptAndAffiliateRollbackWithFailedCredit(t *testing.T) {
	db := useWalletIngressDB(t)
	enableAffiliatePaymentTest(t)
	inviter := User{Username: "gmpay-rollback-inviter", AffCode: "gmpay-rollback-inviter", AffCodeEnabled: true}
	require.NoError(t, db.Create(&inviter).Error)
	user := User{Username: "gmpay-rollback-payer", AffCode: "gmpay-rollback-payer", InviterId: inviter.Id}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "gmpay-rollback-order", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending, CreateTime: time.Now().Unix(), AffiliateBaseQuota: 750_000}
	payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
	injected := errors.New("affiliate insert failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_gmpay_affiliate", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_ledgers" {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("fail_gmpay_affiliate") })
	_, err := RechargeGmpay(payment, "")
	require.ErrorIs(t, err, injected)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, order.CreditedQuota)
	var snapshot GmpayOrder
	require.NoError(t, db.First(&snapshot, order.Id).Error)
	assert.Nil(t, snapshot.GatewayTradeNo)
	assert.Empty(t, snapshot.SettledCryptoAmount)
	assert.Empty(t, snapshot.SettledReceiveAddress)
	assert.Empty(t, snapshot.SettledTransactionID)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&AffiliateLedger{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestGmpayMigrationsAndConcurrentSettlementAcrossDatabases(t *testing.T) {
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
			require.NoError(t, db.AutoMigrate(&User{}, &BillingEvent{}, &TopUp{}, &AffiliateLedger{}, &Log{}, &Option{}, &GmpayOrder{}, &GmpayMerchantVersion{}))
			enableAffiliatePaymentTest(t)
			inviter := User{Username: "gmpay-race-inviter", AffCode: "gmpay-race-inviter", AffCodeEnabled: true}
			require.NoError(t, db.Create(&inviter).Error)
			user := User{Username: "gmpay-race-payer", AffCode: "gmpay-race-payer", InviterId: inviter.Id}
			require.NoError(t, db.Create(&user).Error)

			// Repeated migration of an already-settled installation must keep the
			// frozen snapshot and the settled quota intact.
			preserved := TopUp{UserId: user.Id, TradeNo: "gmpay-preserved-order", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			preservedPayment := insertGmpaySnapshotTestOrder(t, &preserved, 750_000, "preserved-key")
			require.NoError(t, db.AutoMigrate(&GmpayOrder{}, &GmpayMerchantVersion{}))
			var preservedSnapshot GmpayOrder
			require.NoError(t, db.First(&preservedSnapshot, preserved.Id).Error)
			assert.Equal(t, common.Quota(750_000), preservedSnapshot.ExpectedQuota)
			assert.Equal(t, "tron", preservedSnapshot.Network)
			_, err = RechargeGmpay(preservedPayment, "")
			require.NoError(t, err)
			require.NoError(t, db.First(&preserved, preserved.Id).Error)
			assert.Equal(t, common.Quota(750_000), preserved.CreditedQuota)
			require.NoError(t, db.AutoMigrate(&GmpayOrder{}, &GmpayMerchantVersion{}))
			require.NoError(t, db.First(&preserved, preserved.Id).Error)
			assert.Equal(t, common.Quota(750_000), preserved.CreditedQuota)
			assert.Equal(t, common.TopUpStatusSuccess, preserved.Status)
			require.NoError(t, db.First(&preservedSnapshot, preserved.Id).Error)
			assert.Equal(t, preservedPayment.ActualAmount, preservedSnapshot.SettledCryptoAmount)
			assert.Equal(t, preservedPayment.ReceiveAddress, preservedSnapshot.SettledReceiveAddress)
			assert.Equal(t, preservedPayment.BlockTransactionID, preservedSnapshot.SettledTransactionID)

			for index, manual := range []bool{false, true} {
				order := TopUp{UserId: user.Id, TradeNo: fmt.Sprintf("gmpay-race-order-%d", index), Amount: 1, PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending, CreateTime: time.Now().Unix(), AffiliateBaseQuota: 750_000}
				payment := insertGmpaySnapshotTestOrder(t, &order, 750_000, "test-key")
				start := make(chan struct{})
				var wg sync.WaitGroup
				var outcomes [3]error
				wg.Add(3)
				go func() { defer wg.Done(); <-start; _, outcomes[0] = RechargeGmpay(payment, "") }()
				go func() {
					defer wg.Done()
					<-start
					if manual {
						outcomes[1] = ManualCompleteTopUp(order.TradeNo, "")
					} else {
						_, outcomes[1] = RechargeGmpay(payment, "")
					}
				}()
				go func() {
					defer wg.Done()
					<-start
					outcomes[2] = MarkGmpayOrderResult(context.Background(), order.Id, GmpayOrderResult{
						Status: GmpayStatusReady, GatewayTradeNo: payment.GatewayTradeNo, GatewayAmount: "10.00",
						GatewayCryptoAmount: "9.99", ReceiveAddress: "test-address", ExpirationTime: 1893456000,
					})
				}()
				close(start)
				wg.Wait()
				for _, outcome := range outcomes {
					require.NoError(t, outcome)
				}
				require.NoError(t, db.First(&order, order.Id).Error)
				assert.Equal(t, common.Quota(750_000), order.CreditedQuota)
				var snapshot GmpayOrder
				require.NoError(t, db.First(&snapshot, order.Id).Error)
				require.NotNil(t, snapshot.GatewayTradeNo)
				assert.Equal(t, payment.GatewayTradeNo, *snapshot.GatewayTradeNo)
				assert.Equal(t, GmpayStatusSettled, snapshot.Status)
				assert.Equal(t, "10.00", snapshot.GatewayAmount)
				assert.Equal(t, payment.ActualAmount, snapshot.SettledCryptoAmount)
				assert.Equal(t, payment.ReceiveAddress, snapshot.SettledReceiveAddress)
				assert.Equal(t, payment.BlockTransactionID, snapshot.SettledTransactionID)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, common.Quota(2_250_000), user.Quota)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, common.Quota(150_000), inviter.AffQuota)
			var ledgerCount int64
			require.NoError(t, db.Model(&AffiliateLedger{}).Count(&ledgerCount).Error)
			assert.EqualValues(t, 2, ledgerCount)

			duplicate := TopUp{UserId: user.Id, TradeNo: "gmpay-duplicate-receipt", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			duplicatePayment := insertGmpaySnapshotTestOrder(t, &duplicate, 750_000, "rotated-key")
			duplicatePayment.GatewayTradeNo = "receipt-gmpay-race-order-0"
			_, err = RechargeGmpay(duplicatePayment, "")
			require.Error(t, err, "a gateway trade number cannot be claimed by a second local order")
			require.NoError(t, db.First(&duplicate, duplicate.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, duplicate.Status)
			var duplicateSnapshot GmpayOrder
			require.NoError(t, db.First(&duplicateSnapshot, duplicate.Id).Error)
			assert.Nil(t, duplicateSnapshot.GatewayTradeNo)

			pausedOrder := TopUp{UserId: user.Id, TradeNo: "gmpay-pause-wins", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
			pausedPayment := insertGmpaySnapshotTestOrder(t, &pausedOrder, 750_000, "test-key")
			pauseTx := db.Begin()
			require.NoError(t, pauseTx.Error)
			defer pauseTx.Rollback()
			require.NoError(t, pauseTx.Model(&Option{}).Where(&Option{Key: "payment_setting.gmpay_settlement_enabled"}).Update("value", "false").Error)
			attempted := make(chan struct{}, 1)
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("observe_paused_gmpay_settlement", func(tx *gorm.DB) {
				if tx.Statement.Table == "options" {
					select {
					case attempted <- struct{}{}:
					default:
					}
				}
			}))
			t.Cleanup(func() { db.Callback().Create().Remove("observe_paused_gmpay_settlement") })
			settlementResult := make(chan error, 1)
			go func() { _, err := RechargeGmpay(pausedPayment, ""); settlementResult <- err }()
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
				require.ErrorIs(t, err, ErrGmpaySettlementPaused)
			case <-time.After(5 * time.Second):
				t.Fatal("settlement did not finish after pause committed")
			}
			require.NoError(t, db.First(&pausedOrder, pausedOrder.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, pausedOrder.Status)
			assert.Zero(t, pausedOrder.CreditedQuota)
			var pausedSnapshot GmpayOrder
			require.NoError(t, db.First(&pausedSnapshot, pausedOrder.Id).Error)
			assert.Nil(t, pausedSnapshot.GatewayTradeNo)
		})
	}
}
