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
		PaymentProviderEpay: func(n string) error { _, err := RechargeEpay(n, "", ""); return err },
		PaymentProviderStripe: func(n string) error {
			return RechargeWithPayment(n, "customer", "", "", "")
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
				expired := TopUp{UserId: user.Id, TradeNo: "settle-expired-" + provider, PaymentProvider: provider, Status: common.TopUpStatusExpired, ExpectedQuota: 100}
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
