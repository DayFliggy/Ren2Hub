package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

// Replays an already-created order after the operator changes QuotaPerUnit.
// Each provider must credit the checkout-time amount and only once.
func TestFrozenTopUpCreditsOnceUsingNativeProviderFacade(t *testing.T) {
	for _, provider := range []string{PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake} {
		t.Run(provider, func(t *testing.T) {
			db := useGmpayTestDB(t)
			old := common.QuotaPerUnit
			common.QuotaPerUnit = 1000
			t.Cleanup(func() { common.QuotaPerUnit = old })
			user := User{Username: "frozen-" + provider, AffCode: "frozen-" + provider, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			amount := int64(1)
			if provider == PaymentProviderCreem {
				amount = 1000
			}
			order := TopUp{UserId: user.Id, TradeNo: "frozen-" + provider, PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, Amount: amount, Money: 1, Currency: "CNY", PayAmount: "1"}
			require.NoError(t, order.Insert())
			require.Equal(t, 1000, order.ExpectedQuota)
			common.QuotaPerUnit = 2000
			settle := func() error {
				switch provider {
				case PaymentProviderEpay:
					_, err := RechargeEpay(order.TradeNo, "", "", "1")
					return err
				case PaymentProviderStripe:
					return Recharge(order.TradeNo, "", "")
				case PaymentProviderCreem:
					return RechargeCreem(order.TradeNo, "", "", "")
				case PaymentProviderWaffo:
					return RechargeWaffo(order.TradeNo, "")
				default:
					return RechargeWaffoPancake(order.TradeNo)
				}
			}
			require.NoError(t, settle())
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 1000, user.Quota, "a changed exchange scale must not change an already-paid order")
			firstCredit := user.Quota
			retryErr := settle()
			if provider == PaymentProviderStripe || provider == PaymentProviderCreem {
				require.EqualError(t, retryErr, "top-up failed", "native provider facade rejects a completed order")
			} else {
				require.NoError(t, retryErr)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, firstCredit, user.Quota, "a repeated callback must not credit again")
		})
	}
}

func TestFrozenEpayReceiptRejectsMismatchBeforeAndAfterCredit(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "epay-frozen", AffCode: "epay-frozen", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "epay-frozen", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, Amount: 1, Money: 10, Currency: "CNY", PayAmount: "10", ExpectedQuota: 1000}
	require.NoError(t, order.Insert())
	unfrozen := TopUp{UserId: user.Id, TradeNo: "epay-unfrozen", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, ExpectedQuota: 1000}
	require.NoError(t, db.Create(&unfrozen).Error)
	_, unfrozenErr := RechargeEpay(unfrozen.TradeNo, "", "", "10.00")
	require.ErrorIs(t, unfrozenErr, ErrTopUpReceiptMismatch, "an order without a frozen amount cannot be verified")
	require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(unfrozen.TradeNo).Status)
	require.Error(t, ManualCompleteTopUp("epay-missing", ""))
	_, err := RechargeEpay(order.TradeNo, "alipay", "", "9.99")
	require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Zero(t, user.Quota)
	require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
	_, err = RechargeEpay(order.TradeNo, "alipay", "", "10.00")
	require.NoError(t, err)
	_, err = RechargeEpay(order.TradeNo, "alipay", "", "1")
	require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
	already, err := RechargeEpay(order.TradeNo, "alipay", "", "10")
	require.NoError(t, err)
	require.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, 1000, user.Quota)
	saved := GetTopUpByTradeNo(order.TradeNo)
	require.Equal(t, "10", saved.PayAmount)
	require.Equal(t, "CNY", saved.Currency)
	require.Equal(t, 1000, saved.CreditedQuota)
}

// GMPay keeps typed failures because receipt validation and retry recovery are
// part of the local gateway contract; native provider facades are diagnosed separately.
func TestGmpaySettlementKeepsTypedBoundaryErrors(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "gmpay-errors", AffCode: "gmpay-errors", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	foreign := TopUp{UserId: user.Id, TradeNo: "gmpay-foreign", PaymentProvider: PaymentProviderBalance, Status: common.TopUpStatusPending, ExpectedQuota: 100}
	require.NoError(t, db.Create(&foreign).Error)
	settle := func(tradeNo string) error { _, err := RechargeGmpay(GmpayPayment{OrderNo: tradeNo}, ""); return err }
	require.ErrorIs(t, settle("gmpay-missing"), ErrTopUpNotFound)
	require.ErrorIs(t, settle(""), ErrTopUpNotFound)
	require.ErrorIs(t, settle(foreign.TradeNo), ErrPaymentMethodMismatch)
	outage := errors.New("database unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:gmpay-outage", func(tx *gorm.DB) {
		if tx.Statement.Table == "top_ups" {
			tx.AddError(outage)
		}
	}))
	err := settle("gmpay-outage")
	require.NoError(t, db.Callback().Query().Remove("test:gmpay-outage"))
	require.ErrorIs(t, err, outage)
	assert.NotErrorIs(t, err, ErrTopUpNotFound, "a lookup failure must stay retryable")
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(foreign.TradeNo).Status)
}
