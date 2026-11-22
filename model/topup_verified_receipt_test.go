package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVerifiedTopUpReceiptDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openNativeTopUpFacadeDB(t, dialect)
			for _, provider := range []string{PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake} {
				t.Run(provider, func(t *testing.T) {
					user := User{Username: "verified-" + provider, AffCode: "verified-" + provider}
					require.NoError(t, db.Create(&user).Error)
					order := TopUp{UserId: user.Id, TradeNo: "verified-" + provider, PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, ExpectedQuota: 700, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
					require.NoError(t, order.Insert())
					settle := func(receipt TopUpReceipt) error {
						switch provider {
						case PaymentProviderStripe:
							return RechargeVerified(order.TradeNo, "customer", "", receipt)
						case PaymentProviderCreem:
							return RechargeCreemVerified(order.TradeNo, "buyer@example.test", "Buyer", "", receipt)
						case PaymentProviderWaffo:
							return RechargeWaffoVerified(order.TradeNo, "", receipt)
						default:
							return RechargeWaffoPancakeVerified(order.TradeNo, receipt)
						}
					}
					for _, receipt := range []TopUpReceipt{{"USD", "0.99"}, {"EUR", "1"}, {"USD", ""}} {
						require.ErrorIs(t, settle(receipt), ErrTopUpReceiptMismatch)
						require.Equal(t, 0, getUserQuotaFromDB(t, user.Id))
						require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
					}
					require.NoError(t, db.Callback().Update().Before("gorm:update").Register("verified_wallet_failure", func(tx *gorm.DB) { tx.AddError(errors.New("wallet unavailable")) }))
					require.Error(t, settle(TopUpReceipt{"USD", "1"}))
					require.Equal(t, 0, getUserQuotaFromDB(t, user.Id))
					require.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
					require.NoError(t, db.Callback().Update().Remove("verified_wallet_failure"))
					results := make(chan error, 2)
					for i := 0; i < 2; i++ {
						go func() { results <- settle(TopUpReceipt{"USD", "1"}) }()
					}
					for i := 0; i < 2; i++ {
						require.NoError(t, <-results)
					}
					// Verified callbacks acknowledge a matching replay without another credit.
					require.NoError(t, settle(TopUpReceipt{"USD", "1"}))
					require.Equal(t, 700, getUserQuotaFromDB(t, user.Id))
					require.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo(order.TradeNo).Status)
					require.ErrorIs(t, settle(TopUpReceipt{"USD", "2"}), ErrTopUpReceiptMismatch)
					require.Equal(t, 700, getUserQuotaFromDB(t, user.Id))
				})
			}
		})
	}
}
