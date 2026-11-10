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

// TopUpDiscountUsageHourly is the net used_quota change of one user in one UTC hour.
type TopUpDiscountUsageHourly struct {
	UserID  int   `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	HourUTC int64 `gorm:"column:hour_utc;primaryKey;autoIncrement:false;index:idx_topup_discount_usage_hourly_hour"`
	Quota   int64 `gorm:"column:quota;not null;default:0"`
}

func (TopUpDiscountUsageHourly) TableName() string {
	return "topup_discount_usage_hourly"
}

// topUpDiscountQueryTimeout bounds each statement on the request path, so a
// slow database delays a top-up quote by at most this long; a timeout is a failure.
const topUpDiscountQueryTimeout = 3 * time.Second

func topUpDiscountDB() (*gorm.DB, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), topUpDiscountQueryTimeout)
	return DB.WithContext(ctx), cancel
}

// RecordTopUpDiscountUsage mirrors one used_quota change. It writes directly,
// even in batch-update mode, so window sums see it immediately. A failure is
// logged and counted but never fails the already-charged request.
func RecordTopUpDiscountUsage(userID int, quota int) {
	recordTopUpDiscountUsageHourlyAt(userID, quota, time.Now())
}

func recordTopUpDiscountUsageHourlyAt(userID int, quota int, at time.Time) {
	if userID <= 0 || quota == 0 {
		return
	}
	row := TopUpDiscountUsageHourly{UserID: userID, HourUTC: at.Unix() / 3600 * 3600, Quota: int64(quota)}
	db, cancel := topUpDiscountDB()
	defer cancel()
	err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "hour_utc"}},
		DoUpdates: clause.Assignments(map[string]any{
			"quota": gorm.Expr("? + ?", clause.Column{Table: clause.CurrentTable, Name: "quota"}, row.Quota),
		}),
	}).Create(&row).Error
	if err != nil {
		topUpDiscountHealth.hourlyWrite.record()
		common.SysError(common.LogText("failed to record top-up discount usage: user_id=%d, quota=%d, error=%v", userID, quota, err))
	}
}

// TopUpDiscountWindowSums are raw net sums; negative values are possible.
type TopUpDiscountWindowSums struct {
	DayQuota   int64
	WeekQuota  int64
	MonthQuota int64
}

// SumTopUpDiscountWindows sums one user's rows in [monthStart, cutoff).
func SumTopUpDiscountWindows(userID int, dayStart, weekStart, monthStart, cutoff int64) (TopUpDiscountWindowSums, error) {
	var sums TopUpDiscountWindowSums
	db, cancel := topUpDiscountDB()
	defer cancel()
	err := db.Model(&TopUpDiscountUsageHourly{}).
		Select("COALESCE(SUM(CASE WHEN hour_utc >= ? THEN quota ELSE 0 END), 0) AS day_quota, "+
			"COALESCE(SUM(CASE WHEN hour_utc >= ? THEN quota ELSE 0 END), 0) AS week_quota, "+
			"COALESCE(SUM(quota), 0) AS month_quota", dayStart, weekStart).
		Where("user_id = ? AND hour_utc >= ? AND hour_utc < ?", userID, monthStart, cutoff).
		Scan(&sums).Error
	if err != nil {
		topUpDiscountHealth.windowQuery.record()
	}
	return sums, err
}

type TopUpDiscountUserQuota struct {
	UserID int
	Quota  int64
}

// SumTopUpDiscountByUser returns each user's net sum in [start, end).
func SumTopUpDiscountByUser(start, end int64) ([]TopUpDiscountUserQuota, error) {
	var rows []TopUpDiscountUserQuota
	db, cancel := topUpDiscountDB()
	defer cancel()
	err := db.Model(&TopUpDiscountUsageHourly{}).
		Select("user_id, COALESCE(SUM(quota), 0) AS quota").
		Where("hour_utc >= ? AND hour_utc < ?", start, end).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		topUpDiscountHealth.windowQuery.record()
	}
	return rows, err
}

// DeleteTopUpDiscountUsageHourlyBefore removes rows older than the retention cutoff.
func DeleteTopUpDiscountUsageHourlyBefore(hourUTC int64) error {
	db, cancel := topUpDiscountDB()
	defer cancel()
	err := db.Where("hour_utc < ?", hourUTC).Delete(&TopUpDiscountUsageHourly{}).Error
	if err != nil {
		topUpDiscountHealth.cleanup.record()
	}
	return err
}

// EarliestTopUpDiscountHour reports the oldest retained hour, if any.
func EarliestTopUpDiscountHour() (int64, bool, error) {
	var earliest sql.NullInt64
	if err := DB.Model(&TopUpDiscountUsageHourly{}).Select("MIN(hour_utc)").Row().Scan(&earliest); err != nil {
		return 0, false, err
	}
	return earliest.Int64, earliest.Valid, nil
}

type topUpDiscountFailureCounter struct {
	count  atomic.Int64
	lastAt atomic.Int64
}

func (c *topUpDiscountFailureCounter) record() {
	c.count.Add(1)
	c.lastAt.Store(time.Now().Unix())
}

func (c *topUpDiscountFailureCounter) snapshot() TopUpDiscountFailureStat {
	return TopUpDiscountFailureStat{Count: c.count.Load(), LastAt: c.lastAt.Load()}
}

// Process-lifetime failure counters; they reset on restart.
var topUpDiscountHealth struct {
	hourlyWrite topUpDiscountFailureCounter
	windowQuery topUpDiscountFailureCounter
	cleanup     topUpDiscountFailureCounter
}

type TopUpDiscountFailureStat struct {
	Count  int64 `json:"count"`
	LastAt int64 `json:"last_at"`
}

type TopUpDiscountHealth struct {
	HourlyWriteFailures TopUpDiscountFailureStat `json:"hourly_write_failures"`
	WindowQueryFailures TopUpDiscountFailureStat `json:"window_query_failures"`
	CleanupFailures     TopUpDiscountFailureStat `json:"cleanup_failures"`
}

func GetTopUpDiscountHealth() TopUpDiscountHealth {
	return TopUpDiscountHealth{
		HourlyWriteFailures: topUpDiscountHealth.hourlyWrite.snapshot(),
		WindowQueryFailures: topUpDiscountHealth.windowQuery.snapshot(),
		CleanupFailures:     topUpDiscountHealth.cleanup.snapshot(),
	}
}
