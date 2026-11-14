//go:build known_upstream_restored

package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRestoredNativeProviderReplayAcknowledged(t *testing.T) {
	for _, provider := range []string{model.PaymentProviderStripe, model.PaymentProviderCreem} {
		t.Run(provider, func(t *testing.T) {
			db := restoredFundsDB(t)
			user := model.User{Username: "replay-" + provider, AffCode: "replay-" + provider, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			amount := int64(1)
			if provider == model.PaymentProviderCreem {
				amount = 1000
			}
			order := model.TopUp{UserId: user.Id, TradeNo: "replay-" + provider, PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, Amount: amount, Money: 1}
			restoredOrder(t, &order)
			settle := func() error {
				if provider == model.PaymentProviderStripe {
					return model.Recharge(order.TradeNo, "", "")
				}
				return model.RechargeCreem(order.TradeNo, "", "", "")
			}
			require.NoError(t, settle())
			require.NoError(t, db.First(&user, user.Id).Error)
			require.Equal(t, 1000, user.Quota)
			firstCredit := user.Quota
			assert.NoError(t, settle(), "repeated provider delivery must remain acknowledged")
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, firstCredit, user.Quota, "a repeated callback must not credit again")
		})
	}
}
