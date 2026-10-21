package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 每个渠道都从同一个外壳得到带类型的错误，回调处理才能区分永久错误和暂时错误。
func TestSettleTopUpReturnsTypedErrorsForEveryChannel(t *testing.T) {
	db := useGmpayTestDB(t)
	user := User{Username: "settle-errors", AffCode: "settle-errors", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	channels := map[string]func(tradeNo string) error{
		PaymentProviderEpay: func(n string) error { _, err := RechargeEpay(n, "", "", "1.00"); return err },
		PaymentProviderStripe: func(n string) error {
			return RechargeWithPayment(n, "customer", "", "CNY", "1.00")
		},
		PaymentProviderCreem:        func(n string) error { return RechargeCreem(n, "", "", "") },
		PaymentProviderWaffo:        func(n string) error { return RechargeWaffo(n, "") },
		PaymentProviderWaffoPancake: func(n string) error { return RechargeWaffoPancake(n) },
		PaymentProviderGmpay: func(n string) error {
			_, err := RechargeGmpay(GmpayPayment{OrderNo: n}, "")
			return err
		},
	}
	foreign := TopUp{UserId: user.Id, TradeNo: "settle-foreign", PaymentProvider: PaymentProviderBalance, Status: common.TopUpStatusPending, ExpectedQuota: 100}
	require.NoError(t, db.Create(&foreign).Error)
	for provider, settle := range channels {
		t.Run(provider, func(t *testing.T) {
			require.ErrorIs(t, settle("settle-missing-"+provider), ErrTopUpNotFound)
			require.ErrorIs(t, settle(""), ErrTopUpNotFound)
			require.ErrorIs(t, settle(foreign.TradeNo), ErrPaymentMethodMismatch)
			if provider != PaymentProviderGmpay {
				expired := TopUp{UserId: user.Id, TradeNo: "settle-expired-" + provider, PaymentProvider: provider, Status: common.TopUpStatusExpired, ExpectedQuota: 100, Currency: "CNY", PayAmount: "1.00"}
				require.NoError(t, db.Create(&expired).Error)
				require.ErrorIs(t, settle(expired.TradeNo), ErrTopUpStatusInvalid)
			}

			outage := errors.New("database unavailable")
			name := "test:settle-outage-" + provider
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == "top_ups" {
					tx.AddError(outage)
				}
			}))
			err := settle("settle-outage-" + provider)
			require.NoError(t, db.Callback().Query().Remove(name))
			require.ErrorIs(t, err, outage)
			assert.NotErrorIs(t, err, ErrTopUpNotFound, "a lookup failure must stay retryable")
		})
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(foreign.TradeNo).Status)
}

// A2：已验签回执的币种或金额与订单冻结的应付值不一致时不入账，订单保持未完成并写入审计；
// 已完成的订单也不接受另一份回执，冻结值不被回调改写。
func TestSettleTopUpRejectsMismatchedReceipt(t *testing.T) {
	db := useGmpayTestDB(t)
	require.NoError(t, db.AutoMigrate(&AuditLog{}))
	user := User{Username: "receipt-owner", AffCode: "receipt-owner", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := func(tradeNo, provider, currency, amount string) TopUp {
		row := TopUp{UserId: user.Id, TradeNo: tradeNo, PaymentProvider: provider, Status: common.TopUpStatusPending, ExpectedQuota: 1000, Currency: currency, PayAmount: amount, CreateTime: common.GetTimestamp()}
		require.NoError(t, db.Create(&row).Error)
		return row
	}
	epay := order("receipt-epay", PaymentProviderEpay, "CNY", "10.00")
	stripe := order("receipt-stripe", PaymentProviderStripe, "USD", "5")
	unfrozen := order("receipt-unfrozen", PaymentProviderEpay, "", "")

	_, err := RechargeEpay(epay.TradeNo, "", "", "9.99")
	require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
	require.ErrorIs(t, RechargeWithPayment(stripe.TradeNo, "cus", "", "EUR", "5"), ErrTopUpReceiptMismatch)
	require.ErrorIs(t, RechargeWithPayment(stripe.TradeNo, "cus", "", "USD", "4.99"), ErrTopUpReceiptMismatch)
	require.ErrorIs(t, RechargeWithPayment(stripe.TradeNo, "cus", "", "", ""), ErrTopUpReceiptMismatch)
	_, err = RechargeEpay(unfrozen.TradeNo, "", "", "10.00")
	require.ErrorIs(t, err, ErrTopUpReceiptMismatch, "an order without a frozen amount cannot be verified")
	require.Error(t, ManualCompleteTopUp("receipt-missing", ""))

	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	for _, tradeNo := range []string{epay.TradeNo, stripe.TradeNo, unfrozen.TradeNo} {
		assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(tradeNo).Status)
	}
	var audits []AuditLog
	require.NoError(t, db.Where("action = ?", "payment.callback_rejected").Order("id").Find(&audits).Error)
	require.Len(t, audits, 5, "every rejected callback is audited; administrator completion is not a callback")
	assert.Equal(t, user.Id, audits[0].UserId)
	require.NotNil(t, audits[0].Other.Op)
	params, err := common.Marshal(audits[0].Other.Op.Params)
	require.NoError(t, err)
	assert.Contains(t, string(params), `"trade_no":"`+epay.TradeNo+`"`)
	assert.Contains(t, string(params), `"provider":"epay"`)

	// The same value in another decimal spelling is the same amount.
	_, err = RechargeEpay(epay.TradeNo, "", "", "10")
	require.NoError(t, err)
	require.NoError(t, RechargeWithPayment(stripe.TradeNo, "cus", "", "USD", "5.00"))
	_, err = RechargeEpay(epay.TradeNo, "", "", "1.00")
	require.ErrorIs(t, err, ErrTopUpReceiptMismatch, "a completed order does not accept another receipt")
	already, err := RechargeEpay(epay.TradeNo, "", "", "10.00")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 2000, user.Quota)
	settled := GetTopUpByTradeNo(stripe.TradeNo)
	assert.Equal(t, "USD", settled.Currency)
	assert.Equal(t, "5", settled.PayAmount)
}
