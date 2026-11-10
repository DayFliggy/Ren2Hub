package model

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openTopUpDiscountTestDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	switch dialect {
	case "sqlite":
		dialector = sqlite.Open(":memory:")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		dialector = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		dialector = postgres.Open(dsn)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
	}
	previousDB, previousSettings := DB, topUpDiscountSettings.Load()
	DB = db
	topUpDiscountSettings.Store(nil)
	t.Cleanup(func() {
		DB = previousDB
		topUpDiscountSettings.Store(previousSettings)
		require.NoError(t, db.Migrator().DropTable(&TopUpDiscountUsageHourly{}, &TopUpDiscountConfigVersion{}, &TopUpDiscount{}, &GmpayOrder{}, &GmpayMerchantVersion{}, &TopUp{}))
		require.NoError(t, sqlDB.Close())
	})
	tables := []any{&TopUpDiscountUsageHourly{}, &TopUpDiscountConfigVersion{}, &TopUpDiscount{}, &TopUp{}, &GmpayMerchantVersion{}, &GmpayOrder{}}
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.AutoMigrate(tables...), "migration must be repeatable")
	return db
}

func TestTopUpDiscountDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTopUpDiscountTestDB(t, dialect)
			base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
			hour := base.Unix()

			t.Run("hourly upsert adds net changes", func(t *testing.T) {
				recordTopUpDiscountUsageHourlyAt(3, 100, base)
				recordTopUpDiscountUsageHourlyAt(3, 20, base.Add(59*time.Minute))
				recordTopUpDiscountUsageHourlyAt(3, -30, base.Add(30*time.Minute))
				recordTopUpDiscountUsageHourlyAt(3, -40, base.Add(time.Hour))
				recordTopUpDiscountUsageHourlyAt(3, 0, base)
				recordTopUpDiscountUsageHourlyAt(0, 50, base)
				var rows []TopUpDiscountUsageHourly
				require.NoError(t, db.Order("hour_utc").Find(&rows, "user_id = ?", 3).Error)
				assert.Equal(t, []TopUpDiscountUsageHourly{{UserID: 3, HourUTC: hour, Quota: 90}, {UserID: 3, HourUTC: hour + 3600, Quota: -40}}, rows)
				var count int64
				require.NoError(t, db.Model(&TopUpDiscountUsageHourly{}).Count(&count).Error)
				assert.Equal(t, int64(2), count, "zero changes and anonymous users write nothing")
			})

			t.Run("concurrent upserts lose nothing", func(t *testing.T) {
				var wg sync.WaitGroup
				for worker := 0; worker < 10; worker++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for i := 0; i < 100; i++ {
							recordTopUpDiscountUsageHourlyAt(4, 1, base)
						}
					}()
				}
				wg.Wait()
				var row TopUpDiscountUsageHourly
				require.NoError(t, db.First(&row, "user_id = ? AND hour_utc = ?", 4, hour).Error)
				assert.Equal(t, int64(1000), row.Quota)
			})

			cutoff := hour + 40*24*3600
			dayStart, weekStart, monthStart := cutoff-24*3600, cutoff-7*24*3600, cutoff-30*24*3600
			for _, row := range []TopUpDiscountUsageHourly{
				{UserID: 5, HourUTC: cutoff, Quota: 1_000},            // the current day is never counted
				{UserID: 5, HourUTC: cutoff - 3600, Quota: 10},        // last hour before the cutoff
				{UserID: 5, HourUTC: dayStart, Quota: 40},             // first hour of the day window
				{UserID: 5, HourUTC: dayStart - 3600, Quota: -5},      // week only
				{UserID: 5, HourUTC: weekStart, Quota: 7},             // first hour of the week window
				{UserID: 5, HourUTC: monthStart, Quota: 100},          // first hour of the month window
				{UserID: 5, HourUTC: monthStart - 3600, Quota: 9_999}, // before the month window
				{UserID: 6, HourUTC: monthStart - 3600, Quota: 1},
			} {
				require.NoError(t, db.Create(&row).Error)
			}

			t.Run("window sums are left-closed and right-open", func(t *testing.T) {
				sums, err := SumTopUpDiscountWindows(5, dayStart, weekStart, monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, TopUpDiscountWindowSums{DayQuota: 50, WeekQuota: 52, MonthQuota: 152}, sums)
				empty, err := SumTopUpDiscountWindows(99, dayStart, weekStart, monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, TopUpDiscountWindowSums{}, empty)
			})

			t.Run("site sums group by user", func(t *testing.T) {
				rows, err := SumTopUpDiscountByUser(monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, []TopUpDiscountUserQuota{{UserID: 5, Quota: 152}}, rows)
			})

			t.Run("retention deletes only older rows", func(t *testing.T) {
				require.NoError(t, DeleteTopUpDiscountUsageHourlyBefore(monthStart))
				earliest, ok, err := EarliestTopUpDiscountHour()
				require.NoError(t, err)
				require.True(t, ok)
				assert.Equal(t, monthStart, earliest)
				require.NoError(t, db.Where("1 = 1").Delete(&TopUpDiscountUsageHourly{}).Error)
				_, ok, err = EarliestTopUpDiscountHour()
				require.NoError(t, err)
				assert.False(t, ok)
			})

			t.Run("order and discount record commit together", func(t *testing.T) {
				order := func(tradeNo string) *TopUp {
					return &TopUp{UserId: 5, TradeNo: tradeNo, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending,
						Amount: 1, ExpectedQuota: 500000, Currency: "CNY", PayAmount: "6.94",
						Discount: &TopUpDiscount{ConfigVersion: 2, ListPayAmount: "7.3", Decision: `{"factor":"0.95"}`}}
				}
				saved := order("discount-ok")
				require.NoError(t, saved.Insert())
				record, err := GetTopUpDiscountRecord(saved.Id)
				require.NoError(t, err)
				require.NotNil(t, record)
				assert.Equal(t, int64(2), record.ConfigVersion)

				// A second record for the same order id fails, so the order rolls back.
				require.NoError(t, db.Create(&TopUpDiscount{TopUpID: saved.Id + 1, ConfigVersion: 1, ListPayAmount: "1", Decision: "{}"}).Error)
				clash := order("discount-clash")
				require.Error(t, clash.Insert())
				var orders int64
				require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", "discount-clash").Count(&orders).Error)
				assert.Zero(t, orders, "a failed discount insert leaves no order")
				// SQLite may reuse the rolled-back id; drop the blocking row.
				require.NoError(t, db.Where("top_up_id = ?", saved.Id+1).Delete(&TopUpDiscount{}).Error)

				plain := &TopUp{UserId: 5, TradeNo: "no-discount", PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
				require.NoError(t, plain.Insert())
				record, err = GetTopUpDiscountRecord(plain.Id)
				require.NoError(t, err)
				assert.Nil(t, record, "Stripe and Creem orders carry no discount")

				// GMPay writes the snapshot and the record in its own transaction.
				gmpay := &TopUp{UserId: 5, TradeNo: "GPdiscount", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1,
					Discount: &TopUpDiscount{ConfigVersion: 2, ListPayAmount: "7.3", Decision: "{}"}}
				snapshot := &GmpayOrder{AmountMinor: 694, Currency: "CNY", ExpectedQuota: 500000, Token: "usdt", Network: "tron"}
				merchant := &GmpayMerchantVersion{GatewayURL: "https://gateway.example.test", MerchantID: "m", Key: "k"}
				require.NoError(t, CreateGmpayOrder(context.Background(), gmpay, snapshot, merchant))
				record, err = GetTopUpDiscountRecord(gmpay.Id)
				require.NoError(t, err)
				require.NotNil(t, record)

				failing := &TopUp{UserId: 5, TradeNo: "GPdiscount-fail", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1,
					Discount: &TopUpDiscount{ConfigVersion: 2, ListPayAmount: "7.3", Decision: "{}"}}
				require.NoError(t, db.Create(&TopUpDiscount{TopUpID: gmpay.Id + 1, ConfigVersion: 1, ListPayAmount: "1", Decision: "{}"}).Error)
				err = CreateGmpayOrder(context.Background(), failing, &GmpayOrder{AmountMinor: 694, Currency: "CNY", ExpectedQuota: 500000, Token: "usdt", Network: "tron"},
					&GmpayMerchantVersion{GatewayURL: "https://gateway.example.test", MerchantID: "m", Key: "k"})
				require.Error(t, err)
				require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", "GPdiscount-fail").Count(&orders).Error)
				assert.Zero(t, orders, "a failed discount insert rolls back the GMPay order")
				var snapshots int64
				require.NoError(t, db.Model(&GmpayOrder{}).Where("top_up_id = ?", gmpay.Id+1).Count(&snapshots).Error)
				assert.Zero(t, snapshots, "and its payment snapshot")
				require.NoError(t, db.Where("top_up_id = ?", gmpay.Id+1).Delete(&TopUpDiscount{}).Error)
			})

			t.Run("config versions are compare-and-set", func(t *testing.T) {
				settings, err := CurrentTopUpDiscountSettings()
				require.NoError(t, err)
				assert.Zero(t, settings.Version, "an empty table means built-in version 0")
				now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
				data := encodeTopUpDiscountConfig(t, func(c *TopUpDiscountConfig) { c.FixedRate = "0.01" })
				saved, err := SaveTopUpDiscountConfig(data, 0, 9, now)
				require.NoError(t, err)
				assert.Equal(t, int64(1), saved.Version)
				_, err = SaveTopUpDiscountConfig(data, 0, 9, now)
				assert.ErrorIs(t, err, ErrTopUpDiscountVersionConflict)
				_, err = SaveTopUpDiscountConfig(encodeTopUpDiscountConfig(t, func(c *TopUpDiscountConfig) { c.Timezone = "Asia/Kolkata" }), 1, 9, now)
				assert.ErrorIs(t, err, ErrTopUpDiscountInvalidConfig)

				topUpDiscountSettings.Store(nil)
				loaded, err := CurrentTopUpDiscountSettings()
				require.NoError(t, err)
				assert.Equal(t, int64(1), loaded.Version)
				assert.Equal(t, 9, loaded.CreatedBy)
				assert.Equal(t, "0.01", loaded.FixedRate.String())
				var count int64
				require.NoError(t, db.Model(&TopUpDiscountConfigVersion{}).Count(&count).Error)
				assert.Equal(t, int64(1), count)
			})
		})
	}
}
