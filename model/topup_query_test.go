package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// 关键词沿用原生 sanitizeLikePattern：不带 % 时精确匹配，_ 不是通配符，非法模式直接报错。
func TestTopUpKeywordUsesNativeLikePattern(t *testing.T) {
	db, _, invitee := affiliateTestDB(t)
	for _, tradeNo := range []string{"order_1", "orderX1", "order_10"} {
		require.NoError(t, db.Create(&TopUp{UserId: invitee.Id, TradeNo: tradeNo, CreateTime: common.GetTimestamp()}).Error)
	}
	page := &common.PageInfo{Page: 1, PageSize: 10}
	for keyword, want := range map[string]int64{"order_1": 1, "order_1%": 2, "order%": 3, "rder_1": 0} {
		_, total, err := ListTopUps(TopUpFilter{Keyword: keyword}, page)
		require.NoError(t, err, keyword)
		assert.Equal(t, want, total, keyword)
	}
	for _, keyword := range []string{"or%%", "%a%b%c", "%a"} {
		_, _, err := ListTopUps(TopUpFilter{Keyword: keyword}, page)
		assert.Error(t, err, keyword)
	}
}

// 计数最多 searchTopUpCountHardLimit 行；上游的 Limit(n).Count() 不封顶。
func TestListTopUpsCapsCount(t *testing.T) {
	db, _, invitee := affiliateTestDB(t)
	now := common.GetTimestamp()
	rows := make([]TopUp, searchTopUpCountHardLimit+5)
	for i := range rows {
		rows[i] = TopUp{UserId: invitee.Id, TradeNo: fmt.Sprintf("cap-%05d", i), CreateTime: now}
	}
	require.NoError(t, db.CreateInBatches(rows, 500).Error)
	result, total, err := ListTopUps(TopUpFilter{UserID: invitee.Id}, &common.PageInfo{Page: 1, PageSize: 3})
	require.NoError(t, err)
	assert.EqualValues(t, searchTopUpCountHardLimit, total)
	assert.Len(t, result, 3)
}

// 统计按渠道和币种拆分；排行只算成功订单，按到账额度降序、同额按 user_id 升序，并受 limit 限制。
func TestTopUpMethodTotalsAndRanking(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	third := User{Username: "ranking-third", AffCode: "RANKING-THIRD"}
	require.NoError(t, db.Create(&third).Error)
	rows := []TopUp{
		{UserId: invitee.Id, TradeNo: "rank-stripe-1", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Currency: "USD", PayAmount: "1", Status: common.TopUpStatusSuccess, CreditedQuota: 100},
		{UserId: invitee.Id, TradeNo: "rank-stripe-2", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Currency: "USD", PayAmount: "2", Status: common.TopUpStatusSuccess, CreditedQuota: 250},
		{UserId: inviter.Id, TradeNo: "rank-epay", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Currency: "CNY", PayAmount: "7", Status: common.TopUpStatusSuccess, CreditedQuota: 300},
		{UserId: third.Id, TradeNo: "rank-legacy", PaymentMethod: "alipay", Currency: "CNY", PayAmount: "3", Status: common.TopUpStatusSuccess, CreditedQuota: 300},
		{UserId: third.Id, TradeNo: "rank-pending", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Currency: "USD", PayAmount: "9", Status: common.TopUpStatusPending, ExpectedQuota: 900},
	}
	for i := range rows {
		rows[i].CreateTime = common.GetTimestamp()
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	stats, err := GetTopUpStatistics(TopUpFilter{})
	require.NoError(t, err)
	require.Len(t, stats.Methods, 3)
	assert.Equal(t, "alipay", stats.Methods[0].Method, "a legacy order without provider falls back to payment_method")
	assert.Equal(t, "3", stats.Methods[0].PaidAmount)
	assert.Equal(t, PaymentProviderEpay, stats.Methods[1].Method)
	assert.Equal(t, "CNY", stats.Methods[1].Currency)
	assert.Equal(t, PaymentProviderStripe, stats.Methods[2].Method)
	assert.Equal(t, 3, stats.Methods[2].Count)
	assert.Equal(t, 2, stats.Methods[2].PaidCount)
	assert.Equal(t, "3", stats.Methods[2].PaidAmount)

	ranking, err := GetTopUpRanking(TopUpFilter{}, TopUpRankingDefaultLimit)
	require.NoError(t, err)
	require.Len(t, ranking.Items, 3)
	assert.Equal(t, TopUpRankingEntry{UserID: invitee.Id, Username: invitee.Username, PaidCount: 2, CreditedQuota: 350}, ranking.Items[0])
	assert.EqualValues(t, 300, ranking.Items[1].CreditedQuota)
	assert.EqualValues(t, 300, ranking.Items[2].CreditedQuota)
	assert.Less(t, ranking.Items[1].UserID, ranking.Items[2].UserID, "equal quota orders by user_id")
	for _, item := range ranking.Items {
		if item.UserID == third.Id {
			assert.EqualValues(t, 1, item.PaidCount, "pending orders are not ranked")
			assert.Equal(t, third.Username, item.Username)
		}
	}
	limited, err := GetTopUpRanking(TopUpFilter{}, 1)
	require.NoError(t, err)
	assert.Len(t, limited.Items, 1)
	for _, limit := range []int{0, TopUpRankingMaxLimit + 1} {
		_, err = GetTopUpRanking(TopUpFilter{}, limit)
		assert.Error(t, err, limit)
	}
}
