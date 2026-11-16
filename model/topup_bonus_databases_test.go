package model

import (
	"context"
	"os"
	"strconv"
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

func openTopUpBonusTestDB(t *testing.T, dialect string) *gorm.DB {
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
	previousDB, previousSettings, previousType := DB, topUpBonusSettings.Load(), common.MainDatabaseType()
	DB = db
	topUpBonusSettings.Store(nil)
	common.SetMainDatabaseType(map[string]common.DatabaseType{"sqlite": common.DatabaseTypeSQLite, "mysql": common.DatabaseTypeMySQL, "postgres": common.DatabaseTypePostgreSQL}[dialect])
	t.Cleanup(func() {
		DB = previousDB
		topUpBonusSettings.Store(previousSettings)
		common.SetMainDatabaseType(previousType)
		require.NoError(t, db.Migrator().DropTable(&TopUpBonusUsageHourly{}, &TopUpBonusConfigVersion{}, &TopUpBonusDecisionRecord{}, &GmpayOrder{}, &GmpayMerchantVersion{}, &TopUp{}))
		require.NoError(t, sqlDB.Close())
	})
	tables := []any{&TopUpBonusUsageHourly{}, &TopUpBonusConfigVersion{}, &TopUpBonusDecisionRecord{}, &TopUp{}, &GmpayMerchantVersion{}, &GmpayOrder{}}
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.AutoMigrate(tables...), "migration must be repeatable")
	return db
}

func TestTopUpBonusDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openTopUpBonusTestDB(t, dialect)
			base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
			hour := base.Unix()

			t.Run("hourly upsert adds net changes", func(t *testing.T) {
				recordTopUpBonusUsageHourlyAt(3, 100, base)
				recordTopUpBonusUsageHourlyAt(3, 20, base.Add(59*time.Minute))
				recordTopUpBonusUsageHourlyAt(3, -30, base.Add(30*time.Minute))
				recordTopUpBonusUsageHourlyAt(3, -40, base.Add(time.Hour))
				recordTopUpBonusUsageHourlyAt(3, 0, base)
				recordTopUpBonusUsageHourlyAt(0, 50, base)
				var rows []TopUpBonusUsageHourly
				require.NoError(t, db.Order("hour_utc").Find(&rows, "user_id = ?", 3).Error)
				assert.Equal(t, []TopUpBonusUsageHourly{{UserID: 3, HourUTC: hour, Quota: 90}, {UserID: 3, HourUTC: hour + 3600, Quota: -40}}, rows)
				var count int64
				require.NoError(t, db.Model(&TopUpBonusUsageHourly{}).Count(&count).Error)
				assert.Equal(t, int64(2), count, "zero changes and anonymous users write nothing")
			})

			t.Run("concurrent upserts lose nothing", func(t *testing.T) {
				var wg sync.WaitGroup
				for worker := 0; worker < 10; worker++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for i := 0; i < 100; i++ {
							recordTopUpBonusUsageHourlyAt(4, 1, base)
						}
					}()
				}
				wg.Wait()
				var row TopUpBonusUsageHourly
				require.NoError(t, db.First(&row, "user_id = ? AND hour_utc = ?", 4, hour).Error)
				assert.Equal(t, int64(1000), row.Quota)
			})

			cutoff := hour + 40*24*3600
			dayStart, weekStart, monthStart := cutoff-24*3600, cutoff-7*24*3600, cutoff-30*24*3600
			for _, row := range []TopUpBonusUsageHourly{
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
				sums, err := SumTopUpBonusWindows(5, dayStart, weekStart, monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, TopUpBonusWindowSums{DayQuota: 50, WeekQuota: 52, MonthQuota: 152}, sums)
				empty, err := SumTopUpBonusWindows(99, dayStart, weekStart, monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, TopUpBonusWindowSums{}, empty)
			})

			t.Run("site sums group by user", func(t *testing.T) {
				rows, err := SumTopUpBonusByUser(monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, []TopUpBonusUserQuota{{UserID: 5, Quota: 152}}, rows)
			})

			t.Run("retention deletes only older rows", func(t *testing.T) {
				require.NoError(t, DeleteTopUpBonusUsageHourlyBefore(monthStart))
				earliest, ok, err := EarliestTopUpBonusHour()
				require.NoError(t, err)
				require.True(t, ok)
				assert.Equal(t, monthStart, earliest)
				require.NoError(t, db.Where("1 = 1").Delete(&TopUpBonusUsageHourly{}).Error)
				_, ok, err = EarliestTopUpBonusHour()
				require.NoError(t, err)
				assert.False(t, ok)
			})

			t.Run("order and bonus decision commit together", func(t *testing.T) {
				grant := &TopUpBonusGrant{BonusQuota: 25000, ConfigVersion: 2, Decision: `{"bonus_quota":25000}`}
				order := func(tradeNo string) *TopUp {
					return &TopUp{UserId: 5, TradeNo: tradeNo, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending,
						Amount: 1, ExpectedQuota: 525000, BonusQuota: 25000, Currency: "CNY", PayAmount: "7.30", Bonus: grant}
				}
				saved := order("bonus-ok")
				require.NoError(t, saved.Insert())
				assert.Equal(t, 500000, saved.AffiliateBaseQuota, "the referral base is the principal")
				record, err := GetTopUpBonusDecision(TopUpBonusSourceTopUp, saved.Id)
				require.NoError(t, err)
				require.NotNil(t, record)
				assert.Equal(t, int64(2), record.ConfigVersion)
				assert.Equal(t, 5, record.UserID)

				// A second record for the same order id fails, so the order rolls back.
				blocking := &TopUpBonusDecisionRecord{SourceType: TopUpBonusSourceTopUp, SourceID: strconv.Itoa(saved.Id + 1), UserID: 5, ConfigVersion: 1, Decision: "{}"}
				require.NoError(t, db.Create(blocking).Error)
				clash := order("bonus-clash")
				require.Error(t, clash.Insert())
				var orders int64
				require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", "bonus-clash").Count(&orders).Error)
				assert.Zero(t, orders, "a failed decision insert leaves no order")
				// SQLite may reuse the rolled-back id; drop the blocking row.
				require.NoError(t, db.Delete(blocking).Error)

				plain := &TopUp{UserId: 5, TradeNo: "no-bonus", PaymentProvider: PaymentProviderStripe, Status: common.TopUpStatusPending, Amount: 1, Money: 1}
				require.NoError(t, plain.Insert())
				record, err = GetTopUpBonusDecision(TopUpBonusSourceTopUp, plain.Id)
				require.NoError(t, err)
				assert.Nil(t, record, "an order without a decided bonus records nothing")

				// GMPay writes the snapshot and the decision in its own transaction;
				// the snapshot freezes the same full credit as the order.
				gmpay := &TopUp{UserId: 5, TradeNo: "GPbonus", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1,
					ExpectedQuota: 525000, BonusQuota: 25000, Bonus: grant}
				snapshot := &GmpayOrder{AmountMinor: 730, Currency: "CNY", ExpectedQuota: 525000, Token: "usdt", Network: "tron"}
				merchant := &GmpayMerchantVersion{GatewayURL: "https://gateway.example.test", MerchantID: "m", Key: "k"}
				require.NoError(t, CreateGmpayOrder(context.Background(), gmpay, snapshot, merchant))
				assert.Equal(t, 500000, gmpay.AffiliateBaseQuota)
				record, err = GetTopUpBonusDecision(TopUpBonusSourceTopUp, gmpay.Id)
				require.NoError(t, err)
				require.NotNil(t, record)

				mismatched := &TopUp{UserId: 5, TradeNo: "GPbonus-mismatch", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1,
					ExpectedQuota: 525000, BonusQuota: 25000, Bonus: grant}
				err = CreateGmpayOrder(context.Background(), mismatched, &GmpayOrder{AmountMinor: 730, Currency: "CNY", ExpectedQuota: 500000, Token: "usdt", Network: "tron"},
					&GmpayMerchantVersion{GatewayURL: "https://gateway.example.test", MerchantID: "m", Key: "k"})
				require.ErrorIs(t, err, ErrGmpayPaymentMismatch, "the snapshot must freeze the order's full credit")

				failing := &TopUp{UserId: 5, TradeNo: "GPbonus-fail", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1,
					ExpectedQuota: 525000, BonusQuota: 25000, Bonus: grant}
				blocking = &TopUpBonusDecisionRecord{SourceType: TopUpBonusSourceTopUp, SourceID: strconv.Itoa(gmpay.Id + 1), UserID: 5, ConfigVersion: 1, Decision: "{}"}
				require.NoError(t, db.Create(blocking).Error)
				err = CreateGmpayOrder(context.Background(), failing, &GmpayOrder{AmountMinor: 730, Currency: "CNY", ExpectedQuota: 525000, Token: "usdt", Network: "tron"},
					&GmpayMerchantVersion{GatewayURL: "https://gateway.example.test", MerchantID: "m", Key: "k"})
				require.Error(t, err)
				require.NoError(t, db.Model(&TopUp{}).Where("trade_no = ?", "GPbonus-fail").Count(&orders).Error)
				assert.Zero(t, orders, "a failed decision insert rolls back the GMPay order")
				var snapshots int64
				require.NoError(t, db.Model(&GmpayOrder{}).Where("top_up_id = ?", gmpay.Id+1).Count(&snapshots).Error)
				assert.Zero(t, snapshots, "and its payment snapshot")
				require.NoError(t, db.Delete(blocking).Error)
			})

			t.Run("config versions are compare-and-set", func(t *testing.T) {
				settings, err := CurrentTopUpBonusSettings()
				require.NoError(t, err)
				assert.Zero(t, settings.Version, "an empty table means built-in version 0")
				now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
				data := encodeTopUpBonusConfig(t, func(c *TopUpBonusConfig) { c.FixedRate = "0.01" })
				saved, err := SaveTopUpBonusConfig(data, 0, 9, now)
				require.NoError(t, err)
				assert.Equal(t, int64(1), saved.Version)
				_, err = SaveTopUpBonusConfig(data, 0, 9, now)
				assert.ErrorIs(t, err, ErrTopUpBonusVersionConflict)
				_, err = SaveTopUpBonusConfig(encodeTopUpBonusConfig(t, func(c *TopUpBonusConfig) { c.Timezone = "Asia/Kolkata" }), 1, 9, now)
				assert.ErrorIs(t, err, ErrTopUpBonusInvalidConfig)

				topUpBonusSettings.Store(nil)
				loaded, err := CurrentTopUpBonusSettings()
				require.NoError(t, err)
				assert.Equal(t, int64(1), loaded.Version)
				assert.Equal(t, 9, loaded.CreatedBy)
				assert.Equal(t, "0.01", loaded.FixedRate.String())
				var count int64
				require.NoError(t, db.Model(&TopUpBonusConfigVersion{}).Count(&count).Error)
				assert.Equal(t, int64(1), count)
			})

			t.Run("legacy discount data moves once and is dropped", func(t *testing.T) {
				resetTopUpBonusMigrationTables(t, db)
				config := encodeTopUpBonusConfig(t, func(c *TopUpBonusConfig) { c.FixedRate = "0.03" })
				require.NoError(t, db.Create(&[]legacyTopUpDiscountConfig{
					{Version: 1, Config: string(encodeTopUpBonusConfig(t, nil)), CreatedBy: 2, CreatedAt: 100},
					{Version: 4, Config: string(config), CreatedBy: 7, CreatedAt: 400},
				}).Error)
				// One hour spans more than one batch; user 1 already has bonus usage in it.
				var legacy []legacyTopUpDiscountUsage
				for user := 1; user <= topUpBonusUsageMoveBatch+5; user++ {
					legacy = append(legacy, legacyTopUpDiscountUsage{UserID: user, HourUTC: hour, Quota: int64(user)})
				}
				legacy = append(legacy, legacyTopUpDiscountUsage{UserID: 1, HourUTC: hour + 3600, Quota: -3}, legacyTopUpDiscountUsage{UserID: 2, HourUTC: hour + 3600, Quota: 0})
				require.NoError(t, db.CreateInBatches(legacy, 200).Error)
				require.NoError(t, db.Create(&TopUpBonusUsageHourly{UserID: 1, HourUTC: hour, Quota: 10}).Error)

				require.NoError(t, migrateTopUpBonusTables(db))
				require.NoError(t, migrateTopUpBonusTables(db), "a rerun changes nothing")

				var versions []TopUpBonusConfigVersion
				require.NoError(t, db.Order("version").Find(&versions).Error)
				assert.Equal(t, []TopUpBonusConfigVersion{{Version: 1, Config: string(config), CreatedBy: 7, CreatedAt: 400}}, versions,
					"only the latest discount version survives, with its rates and author")
				assertTopUpBonusUsage(t, db, hour, topUpBonusUsageMoveBatch+5, 11)
				assertLegacyTopUpDiscountTablesDropped(t, db)
			})

			t.Run("an interrupted move resumes without counting rows twice", func(t *testing.T) {
				resetTopUpBonusMigrationTables(t, db)
				var legacy []legacyTopUpDiscountUsage
				for user := 1; user <= topUpBonusUsageMoveBatch+5; user++ {
					legacy = append(legacy, legacyTopUpDiscountUsage{UserID: user, HourUTC: hour, Quota: int64(user)})
				}
				legacy = append(legacy, legacyTopUpDiscountUsage{UserID: 1, HourUTC: hour + 3600, Quota: -3}, legacyTopUpDiscountUsage{UserID: 2, HourUTC: hour + 3600, Quota: 0})
				require.NoError(t, db.CreateInBatches(legacy, 200).Error)
				require.NoError(t, db.Create(&TopUpBonusUsageHourly{UserID: 1, HourUTC: hour, Quota: 10}).Error)
				// The first batch committed before the process stopped.
				moved, err := moveTopUpBonusUsageBatch(db)
				require.NoError(t, err)
				require.Equal(t, topUpBonusUsageMoveBatch, moved)

				// A rolled-back release saved its own config; the bonus table keeps its versions.
				require.NoError(t, db.Create(&legacyTopUpDiscountConfig{Version: 9, Config: string(encodeTopUpBonusConfig(t, nil)), CreatedBy: 2, CreatedAt: 900}).Error)
				kept := TopUpBonusConfigVersion{Version: 3, Config: string(encodeTopUpBonusConfig(t, func(c *TopUpBonusConfig) { c.FixedRate = "0.02" })), CreatedBy: 5, CreatedAt: 300}
				require.NoError(t, db.Create(&kept).Error)

				require.NoError(t, migrateTopUpBonusTables(db))
				assertTopUpBonusUsage(t, db, hour, topUpBonusUsageMoveBatch+5, 11)
				var versions []TopUpBonusConfigVersion
				require.NoError(t, db.Find(&versions).Error)
				assert.Equal(t, []TopUpBonusConfigVersion{kept}, versions)
				assertLegacyTopUpDiscountTablesDropped(t, db)
			})

			t.Run("an invalid config stops startup and keeps its source", func(t *testing.T) {
				resetTopUpBonusMigrationTables(t, db)
				require.NoError(t, db.Create(&legacyTopUpDiscountConfig{Version: 2, Config: `{"schema_version":1}`, CreatedBy: 2, CreatedAt: 200}).Error)
				require.Error(t, migrateTopUpBonusTables(db))
				assert.True(t, db.Migrator().HasTable(legacyTopUpDiscountConfigTable))
				var count int64
				require.NoError(t, db.Model(&TopUpBonusConfigVersion{}).Count(&count).Error)
				assert.Zero(t, count)

				// A stored bonus version that no longer loads also keeps the source.
				require.NoError(t, db.Where("1 = 1").Delete(&legacyTopUpDiscountConfig{}).Error)
				require.NoError(t, db.Create(&TopUpBonusConfigVersion{Version: 1, Config: `{}`, CreatedBy: 2, CreatedAt: 1}).Error)
				require.Error(t, migrateTopUpBonusTables(db))
				assert.True(t, db.Migrator().HasTable(legacyTopUpDiscountConfigTable))
				require.NoError(t, db.Where("1 = 1").Delete(&TopUpBonusConfigVersion{}).Error)
				require.NoError(t, migrateTopUpBonusTables(db))
				assertLegacyTopUpDiscountTablesDropped(t, db)
			})
		})
	}
}

// The discount tables as the previous release created them.
type legacyTopUpDiscountConfig struct {
	Version   int64  `gorm:"primaryKey;autoIncrement:false"`
	Config    string `gorm:"type:text;not null"`
	CreatedBy int    `gorm:"not null"`
	CreatedAt int64  `gorm:"not null"`
}

func (legacyTopUpDiscountConfig) TableName() string { return "topup_discount_configs" }

type legacyTopUpDiscountUsage struct {
	UserID  int   `gorm:"primaryKey;autoIncrement:false"`
	HourUTC int64 `gorm:"primaryKey;autoIncrement:false;index:idx_topup_discount_usage_hourly_hour"`
	Quota   int64 `gorm:"not null;default:0"`
}

func (legacyTopUpDiscountUsage) TableName() string { return "topup_discount_usage_hourly" }

type legacyTopUpDiscountRecord struct {
	TopUpID  int    `gorm:"primaryKey;autoIncrement:false"`
	Decision string `gorm:"type:text"`
}

func (legacyTopUpDiscountRecord) TableName() string { return "topup_discounts" }

type legacyUsageDiscountConfig struct {
	Version int64 `gorm:"primaryKey;autoIncrement:false"`
}

func (legacyUsageDiscountConfig) TableName() string { return "usage_discount_configs" }

type legacyUsageDiscountHourly struct {
	UserID int `gorm:"primaryKey;autoIncrement:false"`
}

func (legacyUsageDiscountHourly) TableName() string { return "usage_discount_hourly" }

func resetTopUpBonusMigrationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Where("1 = 1").Delete(&TopUpBonusConfigVersion{}).Error)
	require.NoError(t, db.Where("1 = 1").Delete(&TopUpBonusUsageHourly{}).Error)
	require.NoError(t, db.AutoMigrate(&legacyTopUpDiscountConfig{}, &legacyTopUpDiscountUsage{}, &legacyTopUpDiscountRecord{},
		&legacyUsageDiscountConfig{}, &legacyUsageDiscountHourly{}))
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&legacyTopUpDiscountConfig{}, &legacyTopUpDiscountUsage{}, &legacyTopUpDiscountRecord{},
			&legacyUsageDiscountConfig{}, &legacyUsageDiscountHourly{}))
	})
}

// assertTopUpBonusUsage checks the moved fixture: users 1..users at hour with
// quota equal to the user id (user 1 adds firstHourQuota-1 already stored),
// plus user 1 at -3 and user 2 at 0 in the next hour.
func assertTopUpBonusUsage(t *testing.T, db *gorm.DB, hour int64, users int, firstHourQuota int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&TopUpBonusUsageHourly{}).Count(&count).Error)
	assert.Equal(t, int64(users+2), count)
	var total int64
	require.NoError(t, db.Model(&TopUpBonusUsageHourly{}).Where("hour_utc = ?", hour).Select("COALESCE(SUM(quota), 0)").Scan(&total).Error)
	assert.Equal(t, int64(users*(users+1)/2)+firstHourQuota-1, total)
	var first, negative, zero TopUpBonusUsageHourly
	require.NoError(t, db.First(&first, "user_id = ? AND hour_utc = ?", 1, hour).Error)
	assert.Equal(t, firstHourQuota, first.Quota)
	require.NoError(t, db.First(&negative, "user_id = ? AND hour_utc = ?", 1, hour+3600).Error)
	assert.Equal(t, int64(-3), negative.Quota)
	require.NoError(t, db.First(&zero, "user_id = ? AND hour_utc = ?", 2, hour+3600).Error)
	assert.Zero(t, zero.Quota)
}

func assertLegacyTopUpDiscountTablesDropped(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{legacyTopUpDiscountConfigTable, legacyTopUpDiscountUsageTable, "topup_discounts", "usage_discount_configs", "usage_discount_hourly"} {
		assert.False(t, db.Migrator().HasTable(table), table)
	}
}
