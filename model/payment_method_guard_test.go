package model

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enableAffiliatePaymentTest(t *testing.T) {
	t.Helper()
	previousEnabled := common.AffiliateEnabled
	previousActivatedAt := common.AffiliateActivatedAt
	previousRate := common.AffiliateRebateRateBps
	previousFreeze := common.AffiliateFreezeHours
	previousCap := common.AffiliatePerInviteeCap
	previousQuotaPerUnit := common.QuotaPerUnit
	previousPayment := *operation_setting.GetPaymentSetting()
	common.AffiliateEnabled = true
	common.AffiliateActivatedAt = time.Now().Add(-time.Minute).Unix()
	common.AffiliateRebateRateBps = 1000
	common.AffiliateFreezeHours = 0
	common.AffiliatePerInviteeCap = "100"
	common.QuotaPerUnit = 500_000
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		common.AffiliateEnabled = previousEnabled
		common.AffiliateActivatedAt = previousActivatedAt
		common.AffiliateRebateRateBps = previousRate
		common.AffiliateFreezeHours = previousFreeze
		common.AffiliatePerInviteeCap = previousCap
		common.QuotaPerUnit = previousQuotaPerUnit
		*operation_setting.GetPaymentSetting() = previousPayment
	})
}

func insertUserForPaymentGuardTest(t *testing.T, id int, quota common.Quota) *User {
	t.Helper()
	user := &User{
		Id:       id,
		Username: fmt.Sprintf("payment_guard_user_%d", id),
		Status:   common.UserStatusEnabled,
		Quota:    common.Quota(quota),
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) common.Quota {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("available_quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.EqualValues(t, common.TopUpStatusPending, topUp.Status)
	assert.EqualValues(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.EqualValues(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func createEpayTestOrder(t *testing.T, userId int, tradeNo string, provider string, status string) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          userId,
		Amount:          2,
		Money:           10.0,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          status,
	}
	if provider == PaymentProviderEpay {
		insertEpaySnapshotTestOrder(t, &topUp, 1_000_000, "test-key")
	} else {
		require.NoError(t, DB.Create(&topUp).Error)
	}
	return topUp
}

func insertEpaySnapshotTestOrder(t *testing.T, order *TopUp, quota common.Quota, key string) EpayPayment {
	t.Helper()
	snapshot := EpayOrder{InputAmount: int64(quota), AmountUnit: "TOKENS", AmountMinor: 1000, Currency: "CNY", ExpectedQuota: quota, UnitPrice: "5", GroupRatio: "1", Discount: "1", QuoteFingerprint: "test-quote"}
	merchant := EpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: "merchant-1", Key: key}
	require.NoError(t, CreateEpayOrder(context.Background(), order, &snapshot, &merchant))
	return EpayPayment{OrderNo: order.TradeNo, MerchantID: merchant.MerchantID, MerchantVersionID: merchant.ID, GatewayTradeNo: "receipt-" + order.TradeNo, AmountMinor: snapshot.AmountMinor, PaymentMethod: order.PaymentMethod}
}

func epayPaymentForTest(t *testing.T, order TopUp, method string) EpayPayment {
	t.Helper()
	payment := EpayPayment{OrderNo: order.TradeNo, PaymentMethod: method}
	if order.PaymentProvider != PaymentProviderEpay {
		return payment
	}
	_, snapshot, merchant, err := GetEpayOrder(context.Background(), order.TradeNo)
	require.NoError(t, err)
	payment.MerchantID, payment.MerchantVersionID = merchant.MerchantID, merchant.ID
	payment.GatewayTradeNo, payment.AmountMinor = "receipt-"+order.TradeNo, snapshot.AmountMinor
	return payment
}

func TestRechargeEpayCreditsQuotaExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 501, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTONCE", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.EqualValues(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.EqualValues(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)

	alreadyDone, err = RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.EqualValues(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayReadsCommittedCreditDespiteStaleCache(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 5
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 502, 7)
	require.NoError(t, populateUserCache(*user))
	order := createEpayTestOrder(t, user.Id, "EPAYTESTREDISSYNC", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.EqualValues(t, 1_000_007, getUserQuotaForPaymentGuardTest(t, user.Id))
	available, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(1_000_007), available)

	alreadyDone, err = RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	available, err = GetUserQuota(user.Id, false)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(1_000_007), available)
}

func TestRechargeEpayUpdatesPaymentMethodToActual(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 503, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTMETHOD", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(epayPaymentForTest(t, order, "wxpay"), "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.EqualValues(t, "wxpay", reloaded.PaymentMethod)
	assert.EqualValues(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayRejectsForeignAndNonPendingOrders(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 504, 7)

	t.Run("order from another payment provider", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTSTRIPE", PaymentProviderStripe, common.TopUpStatusPending)
		_, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
		assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
		assert.EqualValues(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("order that is not pending", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTEXPIRED", PaymentProviderEpay, common.TopUpStatusExpired)
		_, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
		assert.ErrorIs(t, err, ErrTopUpStatusInvalid)
		assert.EqualValues(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("missing order", func(t *testing.T) {
		_, err := RechargeEpay(EpayPayment{OrderNo: "EPAYTESTMISSING", PaymentMethod: "alipay"}, "127.0.0.1")
		assert.ErrorIs(t, err, ErrTopUpNotFound)
	})
}

func TestRechargeEpayUsesFrozenQuotaAfterDisplayScaleChange(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = float64(math.MaxInt64)
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 505, 3)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTOVERFLOW", PaymentProviderEpay, common.TopUpStatusPending)

	_, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.EqualValues(t, 1_000_003, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.EqualValues(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRechargeEpayEnforcesFinalWalletQuotaLimit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	testCases := []struct {
		name         string
		currentQuota common.Quota
		wantErr      bool
		wantQuota    common.Quota
		wantStatus   string
	}{
		{
			name:         "allows exact highest representable wallet balance",
			currentQuota: math.MaxInt64 - 1_000_000,
			wantQuota:    math.MaxInt64,
			wantStatus:   common.TopUpStatusSuccess,
		},
		{
			name:         "rejects balance above int64 quota domain",
			currentQuota: math.MaxInt64 - 999_999,
			wantErr:      true,
			wantQuota:    math.MaxInt64 - 999_999,
			wantStatus:   common.TopUpStatusPending,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			user := insertUserForPaymentGuardTest(t, 506, tc.currentQuota)
			order := createEpayTestOrder(t, user.Id, "EPAYTESTWALLETLIMIT", PaymentProviderEpay, common.TopUpStatusPending)

			_, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
			if tc.wantErr {
				require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
			} else {
				require.NoError(t, err)
			}
			assert.EqualValues(t, tc.wantQuota, getUserQuotaForPaymentGuardTest(t, user.Id))
			assert.EqualValues(t, tc.wantStatus, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
		})
	}
}

func TestRechargeEpayCommitsAffiliateRewardExactlyOnce(t *testing.T) {
	truncateTables(t)
	enableAffiliatePaymentTest(t)

	inviter := insertUserForPaymentGuardTest(t, 601, 0)
	require.NoError(t, DB.Model(inviter).Updates(map[string]any{
		"aff_code": "PAYMENT-INVITER", "aff_code_enabled": true,
	}).Error)
	invitee := insertUserForPaymentGuardTest(t, 602, 0)
	require.NoError(t, DB.Model(invitee).Update("inviter_id", inviter.Id).Error)
	order := createEpayTestOrder(t, invitee.Id, "EPAYAFFILIATEONCE", PaymentProviderEpay, common.TopUpStatusPending)
	require.NoError(t, DB.Model(&order).Update("affiliate_base_quota", 1_000_000).Error)

	alreadyDone, err := RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.EqualValues(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, invitee.Id))

	require.NoError(t, DB.First(inviter, inviter.Id).Error)
	assert.EqualValues(t, 100_000, inviter.AffQuota)
	assert.EqualValues(t, 100_000, inviter.AffHistoryQuota)
	var ledgerCount int64
	require.NoError(t, DB.Model(&AffiliateLedger{}).Where("source_event_key = ?", "topup:"+strconv.Itoa(order.Id)).Count(&ledgerCount).Error)
	assert.EqualValues(t, 1, ledgerCount)

	alreadyDone, err = RechargeEpay(epayPaymentForTest(t, order, "alipay"), "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	require.NoError(t, DB.Model(&AffiliateLedger{}).Where("source_event_key = ?", "topup:"+strconv.Itoa(order.Id)).Count(&ledgerCount).Error)
	assert.EqualValues(t, 1, ledgerCount)
}

// TestRechargeGmpayRejectsForeignAndNonPendingOrders extends the provider guard
// to the crypto rail: a snapshot-bound order may be settled only by the provider
// that created it, and only while it is still pending.
func TestRechargeGmpayRejectsForeignAndNonPendingOrders(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 701, 7)

	t.Run("crypto order from another provider", func(t *testing.T) {
		insertTopUpForPaymentGuardTest(t, "GMPAYGUARDEPAY", user.Id, PaymentProviderEpay)
		_, err := RechargeGmpay(GmpayPayment{OrderNo: "GMPAYGUARDEPAY", GatewayTradeNo: "receipt"}, "127.0.0.1")
		assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
		assert.EqualValues(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("provider switch is refused for a crypto order", func(t *testing.T) {
		order := &TopUp{
			UserId: user.Id, Amount: 10, Money: 10, TradeNo: "GMPAYGUARDSWITCH",
			PaymentMethod: "gmpay:usdt.tron", PaymentProvider: PaymentProviderGmpay,
			CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
		}
		snapshot := &GmpayOrder{
			InputAmount: 10, AmountUnit: "USD", AmountMinor: 1000, Currency: "USD",
			ExpectedQuota: 5_000_000, UnitPrice: "1", GroupRatio: "1", Discount: "1",
			Token: "usdt", Network: "tron", QuoteFingerprint: "guard-quote",
		}
		merchant := &GmpayMerchantVersion{
			GatewayURL: "https://payments.example.test", MerchantID: "1", Key: "guard-key",
		}
		require.NoError(t, CreateGmpayOrder(context.Background(), order, snapshot, merchant))

		err := UpdatePendingTopUpStatus(order.TradeNo, PaymentProviderEpay, common.TopUpStatusExpired)
		assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
		assert.EqualValues(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))

		_, err = RechargeEpay(EpayPayment{OrderNo: order.TradeNo, PaymentMethod: "alipay"}, "127.0.0.1")
		assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
		assert.EqualValues(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})
}

// TestUpdatePendingTopUpStatusRejectsMismatchedGmpayProvider pins that the
// status guard also refuses a crypto order whose stored provider is gmpay when
// another rail claims it.
func TestUpdatePendingTopUpStatusRejectsMismatchedGmpayProvider(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 702, 0)
	insertTopUpForPaymentGuardTest(t, "gmpay-expire-guard", 702, PaymentProviderGmpay)

	err := UpdatePendingTopUpStatus("gmpay-expire-guard", PaymentProviderStripe, common.TopUpStatusExpired)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.EqualValues(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "gmpay-expire-guard"))
}
