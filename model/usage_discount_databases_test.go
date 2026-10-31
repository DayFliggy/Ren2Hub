package model

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openUsageDiscountTestDB(t *testing.T, dialect string) *gorm.DB {
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
	previousDB, previousSettings := DB, usageDiscountSettings.Load()
	DB = db
	usageDiscountSettings.Store(nil)
	t.Cleanup(func() {
		DB = previousDB
		usageDiscountSettings.Store(previousSettings)
		require.NoError(t, db.Migrator().DropTable(&UsageDiscountHourly{}, &UsageDiscountConfigVersion{}))
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&UsageDiscountHourly{}, &UsageDiscountConfigVersion{}))
	require.NoError(t, db.AutoMigrate(&UsageDiscountHourly{}, &UsageDiscountConfigVersion{}), "migration must be repeatable")
	return db
}

func TestUsageDiscountDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openUsageDiscountTestDB(t, dialect)
			base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
			hour := base.Unix()

			t.Run("hourly upsert adds net changes", func(t *testing.T) {
				recordUsageDiscountHourlyAt(3, 100, base)
				recordUsageDiscountHourlyAt(3, 20, base.Add(59*time.Minute))
				recordUsageDiscountHourlyAt(3, -30, base.Add(30*time.Minute))
				recordUsageDiscountHourlyAt(3, -40, base.Add(time.Hour))
				recordUsageDiscountHourlyAt(3, 0, base)
				recordUsageDiscountHourlyAt(0, 50, base)
				var rows []UsageDiscountHourly
				require.NoError(t, db.Order("hour_utc").Find(&rows, "user_id = ?", 3).Error)
				assert.Equal(t, []UsageDiscountHourly{{UserID: 3, HourUTC: hour, Quota: 90}, {UserID: 3, HourUTC: hour + 3600, Quota: -40}}, rows)
				var count int64
				require.NoError(t, db.Model(&UsageDiscountHourly{}).Count(&count).Error)
				assert.Equal(t, int64(2), count, "zero changes and anonymous users write nothing")
			})

			t.Run("concurrent upserts lose nothing", func(t *testing.T) {
				var wg sync.WaitGroup
				for worker := 0; worker < 10; worker++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for i := 0; i < 100; i++ {
							recordUsageDiscountHourlyAt(4, 1, base)
						}
					}()
				}
				wg.Wait()
				var row UsageDiscountHourly
				require.NoError(t, db.First(&row, "user_id = ? AND hour_utc = ?", 4, hour).Error)
				assert.Equal(t, int64(1000), row.Quota)
			})

			cutoff := hour + 40*24*3600
			dayStart, weekStart, monthStart := cutoff-24*3600, cutoff-7*24*3600, cutoff-30*24*3600
			for _, row := range []UsageDiscountHourly{
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
				sums, err := SumUsageDiscountWindows(5, dayStart, weekStart, monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, UsageDiscountWindowSums{DayQuota: 50, WeekQuota: 52, MonthQuota: 152}, sums)
				empty, err := SumUsageDiscountWindows(99, dayStart, weekStart, monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, UsageDiscountWindowSums{}, empty)
			})

			t.Run("site sums group by user", func(t *testing.T) {
				rows, err := SumUsageDiscountByUser(monthStart, cutoff)
				require.NoError(t, err)
				assert.Equal(t, []UsageDiscountUserQuota{{UserID: 5, Quota: 152}}, rows)
			})

			t.Run("retention deletes only older rows", func(t *testing.T) {
				require.NoError(t, DeleteUsageDiscountHourlyBefore(monthStart))
				earliest, ok, err := EarliestUsageDiscountHour()
				require.NoError(t, err)
				require.True(t, ok)
				assert.Equal(t, monthStart, earliest)
				require.NoError(t, db.Where("1 = 1").Delete(&UsageDiscountHourly{}).Error)
				_, ok, err = EarliestUsageDiscountHour()
				require.NoError(t, err)
				assert.False(t, ok)
			})

			t.Run("config versions are compare-and-set", func(t *testing.T) {
				settings, err := CurrentUsageDiscountSettings()
				require.NoError(t, err)
				assert.Zero(t, settings.Version, "an empty table means built-in version 0")
				now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
				data := encodeUsageDiscountConfig(t, func(c *UsageDiscountConfig) { c.FixedRate = "0.01" })
				saved, err := SaveUsageDiscountConfig(data, 0, 9, now)
				require.NoError(t, err)
				assert.Equal(t, int64(1), saved.Version)
				_, err = SaveUsageDiscountConfig(data, 0, 9, now)
				assert.ErrorIs(t, err, ErrUsageDiscountVersionConflict)
				_, err = SaveUsageDiscountConfig(encodeUsageDiscountConfig(t, func(c *UsageDiscountConfig) { c.Timezone = "Asia/Kolkata" }), 1, 9, now)
				assert.ErrorIs(t, err, ErrUsageDiscountInvalidConfig)

				usageDiscountSettings.Store(nil)
				loaded, err := CurrentUsageDiscountSettings()
				require.NoError(t, err)
				assert.Equal(t, int64(1), loaded.Version)
				assert.Equal(t, 9, loaded.CreatedBy)
				assert.Equal(t, "0.01", loaded.FixedRate.String())
				var count int64
				require.NoError(t, db.Model(&UsageDiscountConfigVersion{}).Count(&count).Error)
				assert.Equal(t, int64(1), count)
			})
		})
	}
}
