package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// 排行的 SUM/GROUP BY/ORDER BY SUM、原生 COUNT 和 ESCAPE '!' 都是方言敏感的
// SQL：PostgreSQL 的 SUM(bigint) 返回 numeric，MySQL 返回 DECIMAL，都要能扫描进 int64。
func TestTopUpQueryDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			dbType := common.DatabaseTypeSQLite
			if dialect != "sqlite" {
				key := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
				if dialect == "postgres" {
					key = "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
				}
				dsn := os.Getenv(key)
				if dsn == "" {
					t.Skip(key + " is not configured; isolated database required")
				}
				driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
				if dialect == "postgres" {
					driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("topupq_%d_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				connection.SetMaxOpenConns(1)
			}
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(dbType)
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				require.NoError(t, db.Migrator().DropTable(&TopUp{}, &User{}))
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}))
			verifyTopUpQueries(t, db)
		})
	}
}

func verifyTopUpQueries(t *testing.T, db *gorm.DB) {
	t.Helper()
	first := User{Username: "topupq-first", AffCode: "TOPUPQ-FIRST"}
	second := User{Username: "topupq-second", AffCode: "TOPUPQ-SECOND"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	now := common.GetTimestamp()
	// 两笔 1.5e9 的到账额度之和超过 int32，确认三种数据库都按 int64 汇总。
	rows := []TopUp{
		{UserId: first.Id, TradeNo: "order_1", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Currency: "USD", PayAmount: "1.25", Status: common.TopUpStatusSuccess, CreditedQuota: 1_500_000_000},
		{UserId: first.Id, TradeNo: "order_10", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Currency: "USD", PayAmount: "2", Status: common.TopUpStatusSuccess, CreditedQuota: 1_500_000_000},
		{UserId: second.Id, TradeNo: "orderX1", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Currency: "CNY", PayAmount: "7", Status: common.TopUpStatusSuccess, CreditedQuota: 300},
		{UserId: second.Id, TradeNo: "order-pending", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Currency: "USD", PayAmount: "9", Status: common.TopUpStatusPending, ExpectedQuota: 900},
	}
	for i := range rows {
		rows[i].CreateTime = now
		require.NoError(t, db.Create(&rows[i]).Error)
	}

	ranking, err := GetTopUpRanking(TopUpFilter{}, TopUpRankingDefaultLimit)
	require.NoError(t, err)
	require.Equal(t, []TopUpRankingEntry{
		{UserID: first.Id, Username: first.Username, PaidCount: 2, CreditedQuota: 3_000_000_000},
		{UserID: second.Id, Username: second.Username, PaidCount: 1, CreditedQuota: 300},
	}, ranking.Items)

	stats, err := GetTopUpStatistics(TopUpFilter{}, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, 4, stats.Total)
	require.Len(t, stats.Currencies, 2)
	assert.Equal(t, TopUpCurrencyTotal{Currency: "USD", Count: 3, PaidCount: 2, PaidAmount: "3.25", CreditedQuota: 3_000_000_000}, stats.Currencies[1])

	page := &common.PageInfo{Page: 1, PageSize: 10}
	for keyword, want := range map[string]int64{"order_1": 1, "order_1%": 2, "order%": 4, "rder_1": 0} {
		_, total, err := ListTopUps(TopUpFilter{Keyword: keyword}, page)
		require.NoError(t, err, keyword)
		assert.Equal(t, want, total, keyword)
	}
	verifyTopUpMethodScope(t, db)
}

// verifyTopUpMethodScope 确认 SQL 筛选（topUpMethodScope）与 Go 归类（topUpEffectiveMethod）
// 对同一批订单给出同一划分，包括两列为 NULL 或空串、未知 provider 覆盖已知 method 的情况。
func verifyTopUpMethodScope(t *testing.T, db *gorm.DB) {
	t.Helper()
	owner := User{Username: "topupq-methods", AffCode: "TOPUPQ-METHODS"}
	require.NoError(t, db.Create(&owner).Error)
	rows := []TopUp{
		{TradeNo: "method-provider", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe},
		{TradeNo: "method-legacy-known", PaymentMethod: PaymentMethodStripe},
		{TradeNo: "method-provider-wins", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay"},
		{TradeNo: "method-legacy-unknown", PaymentMethod: "alipay"},
		{TradeNo: "method-unknown-provider", PaymentProvider: "paypal", PaymentMethod: PaymentMethodStripe},
		{TradeNo: "method-empty"},
		{TradeNo: "method-null-provider", PaymentMethod: PaymentMethodCreem},
		{TradeNo: "method-null-both"},
	}
	for i := range rows {
		rows[i].UserId, rows[i].CreateTime = owner.Id, common.GetTimestamp()
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	nullProvider := []string{"method-null-provider", "method-null-both"}
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no IN ?", nullProvider).Update("payment_provider", gorm.Expr("NULL")).Error)
	require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", "method-null-both").Update("payment_method", gorm.Expr("NULL")).Error)

	want := map[string][]string{
		PaymentProviderStripe: {"method-provider", "method-legacy-known"},
		PaymentProviderEpay:   {"method-provider-wins"},
		PaymentProviderCreem:  {"method-null-provider"},
		TopUpMethodOther:      {"method-legacy-unknown", "method-unknown-provider", "method-empty", "method-null-both"},
	}
	page := &common.PageInfo{Page: 1, PageSize: 20}
	for _, method := range append([]string{TopUpMethodOther}, topUpKnownMethods...) {
		found, total, err := ListTopUps(TopUpFilter{UserID: owner.Id, Method: method}, page)
		require.NoError(t, err, method)
		got := make([]string, 0, len(found))
		for _, row := range found {
			assert.Equal(t, method, topUpEffectiveMethod(row.PaymentProvider, row.PaymentMethod), row.TradeNo)
			got = append(got, row.TradeNo)
		}
		assert.ElementsMatch(t, want[method], got, method)
		assert.EqualValues(t, len(want[method]), total, method)
	}

	stats, err := GetTopUpStatistics(TopUpFilter{UserID: owner.Id}, time.UTC)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, total := range stats.Methods {
		counts[total.Method] += total.Count
	}
	assert.Equal(t, map[string]int{PaymentProviderStripe: 2, PaymentProviderEpay: 1, PaymentProviderCreem: 1, TopUpMethodOther: 4}, counts)

	_, _, err = ListTopUps(TopUpFilter{Method: "alipay"}, page)
	assert.Error(t, err, "a raw payment_method is not a filter value")
}
