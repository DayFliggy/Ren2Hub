//go:build known_upstream_restored

package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRestoredNativeUpstreamSyncStripeRetriesTransientWalletFailure(t *testing.T) {
	db := restoredFundsDB(t)
	deliver := restoredStripeWebhook(t)
	user := model.User{Username: "stripe-outage", AffCode: "stripe-outage", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "stripe-outage", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
	restoredOrder(t, &order)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("upstream-sync:stripe-outage", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("temporary wallet failure"))
		}
	}))
	payload := restoredStripeEvent(`"mode":"payment","client_reference_id":"stripe-outage","amount_subtotal":100,"currency":"usd"`)
	assert.Equal(t, http.StatusInternalServerError, deliver(payload), "ACK would lose a successful payment when the wallet temporarily fails")
	require.NoError(t, db.Callback().Update().Remove("upstream-sync:stripe-outage"))
	assert.Equal(t, http.StatusOK, deliver(payload))
	assert.Equal(t, http.StatusOK, deliver(payload))
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
}
