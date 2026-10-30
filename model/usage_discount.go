package model

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UsageDiscountHourly is the net used_quota change of one user in one UTC hour.
type UsageDiscountHourly struct {
	UserID  int   `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	HourUTC int64 `gorm:"column:hour_utc;primaryKey;autoIncrement:false;index:idx_usage_discount_hourly_hour"`
	Quota   int64 `gorm:"column:quota;not null;default:0"`
}

func (UsageDiscountHourly) TableName() string {
	return "usage_discount_hourly"
}

// usageDiscountQueryTimeout bounds each statement on the request path, so a
// slow database delays billing by at most this long; a timeout is a failure.
const usageDiscountQueryTimeout = 3 * time.Second

func usageDiscountDB() (*gorm.DB, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), usageDiscountQueryTimeout)
	return DB.WithContext(ctx), cancel
}

// RecordUsageDiscountHourly mirrors one used_quota change. It writes directly,
// even in batch-update mode, so window sums see it immediately. A failure is
// logged and counted but never fails the already-charged request.
func RecordUsageDiscountHourly(userID int, quota int) {
	recordUsageDiscountHourlyAt(userID, quota, time.Now())
}

func recordUsageDiscountHourlyAt(userID int, quota int, at time.Time) {
	if userID <= 0 || quota == 0 {
		return
	}
	row := UsageDiscountHourly{UserID: userID, HourUTC: at.Unix() / 3600 * 3600, Quota: int64(quota)}
	db, cancel := usageDiscountDB()
	defer cancel()
	err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "hour_utc"}},
		DoUpdates: clause.Assignments(map[string]any{
			"quota": gorm.Expr("? + ?", clause.Column{Table: clause.CurrentTable, Name: "quota"}, row.Quota),
		}),
	}).Create(&row).Error
	if err != nil {
		usageDiscountHealth.hourlyWrite.record()
		common.SysError(common.LogText("failed to record usage discount hourly quota: user_id=%d, quota=%d, error=%v", userID, quota, err))
	}
}

// UsageDiscountWindowSums are raw net sums; negative values are possible.
type UsageDiscountWindowSums struct {
	DayQuota   int64
	WeekQuota  int64
	MonthQuota int64
}

// SumUsageDiscountWindows sums one user's rows in [monthStart, cutoff).
func SumUsageDiscountWindows(userID int, dayStart, weekStart, monthStart, cutoff int64) (UsageDiscountWindowSums, error) {
	var sums UsageDiscountWindowSums
	db, cancel := usageDiscountDB()
	defer cancel()
	err := db.Model(&UsageDiscountHourly{}).
		Select("COALESCE(SUM(CASE WHEN hour_utc >= ? THEN quota ELSE 0 END), 0) AS day_quota, "+
			"COALESCE(SUM(CASE WHEN hour_utc >= ? THEN quota ELSE 0 END), 0) AS week_quota, "+
			"COALESCE(SUM(quota), 0) AS month_quota", dayStart, weekStart).
		Where("user_id = ? AND hour_utc >= ? AND hour_utc < ?", userID, monthStart, cutoff).
		Scan(&sums).Error
	if err != nil {
		usageDiscountHealth.windowQuery.record()
	}
	return sums, err
}

type UsageDiscountUserQuota struct {
	UserID int
	Quota  int64
}

// SumUsageDiscountByUser returns each user's net sum in [start, end).
func SumUsageDiscountByUser(start, end int64) ([]UsageDiscountUserQuota, error) {
	var rows []UsageDiscountUserQuota
	db, cancel := usageDiscountDB()
	defer cancel()
	err := db.Model(&UsageDiscountHourly{}).
		Select("user_id, COALESCE(SUM(quota), 0) AS quota").
		Where("hour_utc >= ? AND hour_utc < ?", start, end).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		usageDiscountHealth.windowQuery.record()
	}
	return rows, err
}

// DeleteUsageDiscountHourlyBefore removes rows older than the retention cutoff.
func DeleteUsageDiscountHourlyBefore(hourUTC int64) error {
	err := DB.Where("hour_utc < ?", hourUTC).Delete(&UsageDiscountHourly{}).Error
	if err != nil {
		usageDiscountHealth.cleanup.record()
	}
	return err
}

// EarliestUsageDiscountHour reports the oldest retained hour, if any.
func EarliestUsageDiscountHour() (int64, bool, error) {
	var earliest sql.NullInt64
	if err := DB.Model(&UsageDiscountHourly{}).Select("MIN(hour_utc)").Row().Scan(&earliest); err != nil {
		return 0, false, err
	}
	return earliest.Int64, earliest.Valid, nil
}

type usageDiscountFailureCounter struct {
	count  atomic.Int64
	lastAt atomic.Int64
}

func (c *usageDiscountFailureCounter) record() {
	c.count.Add(1)
	c.lastAt.Store(time.Now().Unix())
}

func (c *usageDiscountFailureCounter) snapshot() UsageDiscountFailureStat {
	return UsageDiscountFailureStat{Count: c.count.Load(), LastAt: c.lastAt.Load()}
}

// Process-lifetime failure counters; they reset on restart.
var usageDiscountHealth struct {
	hourlyWrite usageDiscountFailureCounter
	windowQuery usageDiscountFailureCounter
	cleanup     usageDiscountFailureCounter
}

type UsageDiscountFailureStat struct {
	Count  int64 `json:"count"`
	LastAt int64 `json:"last_at"`
}

type UsageDiscountHealth struct {
	HourlyWriteFailures UsageDiscountFailureStat `json:"hourly_write_failures"`
	WindowQueryFailures UsageDiscountFailureStat `json:"window_query_failures"`
	CleanupFailures     UsageDiscountFailureStat `json:"cleanup_failures"`
}

func GetUsageDiscountHealth() UsageDiscountHealth {
	return UsageDiscountHealth{
		HourlyWriteFailures: usageDiscountHealth.hourlyWrite.snapshot(),
		WindowQueryFailures: usageDiscountHealth.windowQuery.snapshot(),
		CleanupFailures:     usageDiscountHealth.cleanup.snapshot(),
	}
}
