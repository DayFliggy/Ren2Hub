package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTopUpQueryFiltersOwnershipAndCurrencyTotals(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	rows := []TopUp{
		{UserId: invitee.Id, TradeNo: "usd-success", PaymentMethod: "stripe", Currency: "USD", PayAmount: "0.1", Status: common.TopUpStatusSuccess, CreditedQuota: 10},
		{UserId: invitee.Id, TradeNo: "usd-success-2", PaymentMethod: "stripe", Currency: "USD", PayAmount: "0.2", Status: common.TopUpStatusSuccess, CreditedQuota: 20},
		{UserId: inviter.Id, TradeNo: "cny-success", PaymentMethod: "gmpay:usdt.tron", Currency: "CNY", PayAmount: "7.23", Status: common.TopUpStatusSuccess, CreditedQuota: 30},
		{UserId: invitee.Id, TradeNo: "usd-pending", PaymentMethod: "stripe", Currency: "USD", PayAmount: "99", Status: common.TopUpStatusPending},
		{UserId: invitee.Id, TradeNo: "legacy-success", PaymentMethod: "epay", Status: common.TopUpStatusSuccess},
	}
	for i := range rows {
		rows[i].CreateTime = common.GetTimestamp()
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	page := &common.PageInfo{Page: 1, PageSize: 2}
	result, total, err := ListTopUps(TopUpFilter{UserID: invitee.Id, Status: common.TopUpStatusSuccess, Method: "stripe", Currency: "USD"}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	assert.Len(t, result, 2)
	_, err = GetTopUpDetail("cny-success", invitee.Id)
	require.Error(t, err)
	stats, err := GetTopUpStatistics(TopUpFilter{})
	require.NoError(t, err)
	assert.Equal(t, 5, stats.Total)
	require.Len(t, stats.Currencies, 3)
	assert.Equal(t, "", stats.Currencies[0].Currency)
	assert.Equal(t, 1, stats.Currencies[0].UnknownAmountCount)
	assert.Equal(t, "CNY", stats.Currencies[1].Currency)
	assert.Equal(t, "7.23", stats.Currencies[1].PaidAmount)
	assert.Equal(t, "USD", stats.Currencies[2].Currency)
	assert.Equal(t, "0.3", stats.Currencies[2].PaidAmount)
	assert.Equal(t, 2, stats.Currencies[2].PaidCount)
}
