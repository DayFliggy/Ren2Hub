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

// TopUpBonusUsageHourly is the net used_quota change of one user in one UTC hour.
type TopUpBonusUsageHourly struct {
	UserID  int   `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	HourUTC int64 `gorm:"column:hour_utc;primaryKey;autoIncrement:false;index:idx_topup_bonus_usage_hourly_hour"`
	Quota   int64 `gorm:"column:quota;not null;default:0"`
}

func (TopUpBonusUsageHourly) TableName() string {
	return "topup_bonus_usage_hourly"
}

// topUpBonusQueryTimeout bounds each statement on the request path, so a
// slow database delays a top-up quote by at most this long; a timeout is a failure.
const topUpBonusQueryTimeout = 3 * time.Second

func topUpBonusDB() (*gorm.DB, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), topUpBonusQueryTimeout)
	return DB.WithContext(ctx), cancel
}

// RecordTopUpBonusUsage mirrors one used_quota change. It writes directly,
// even in batch-update mode, so window sums see it immediately. A failure is
// logged and counted but never fails the already-charged request.
func RecordTopUpBonusUsage(userID int, quota int) {
	recordTopUpBonusUsageHourlyAt(userID, quota, time.Now())
}

func recordTopUpBonusUsageHourlyAt(userID int, quota int, at time.Time) {
	if userID <= 0 || quota == 0 {
		return
	}
	db, cancel := topUpBonusDB()
	defer cancel()
	err := addTopUpBonusUsageHourly(db, TopUpBonusUsageHourly{UserID: userID, HourUTC: at.Unix() / 3600 * 3600, Quota: int64(quota)})
	if err != nil {
		topUpBonusHealth.hourlyWrite.record()
		common.SysError(common.LogText("failed to record top-up bonus usage: user_id=%d, quota=%d, error=%v", userID, quota, err))
	}
}

// addTopUpBonusUsageHourly adds one row's quota to the stored hour, creating
// it when absent. The migration reuses it to move legacy rows one by one.
func addTopUpBonusUsageHourly(db *gorm.DB, row TopUpBonusUsageHourly) error {
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "hour_utc"}},
		DoUpdates: clause.Assignments(map[string]any{
			"quota": gorm.Expr("? + ?", clause.Column{Table: clause.CurrentTable, Name: "quota"}, row.Quota),
		}),
	}).Create(&row).Error
}

// TopUpBonusWindowSums are raw net sums; negative values are possible.
type TopUpBonusWindowSums struct {
	DayQuota   int64
	WeekQuota  int64
	MonthQuota int64
}

// SumTopUpBonusWindows sums one user's rows in [monthStart, cutoff).
func SumTopUpBonusWindows(userID int, dayStart, weekStart, monthStart, cutoff int64) (TopUpBonusWindowSums, error) {
	var sums TopUpBonusWindowSums
	db, cancel := topUpBonusDB()
	defer cancel()
	err := db.Model(&TopUpBonusUsageHourly{}).
		Select("COALESCE(SUM(CASE WHEN hour_utc >= ? THEN quota ELSE 0 END), 0) AS day_quota, "+
			"COALESCE(SUM(CASE WHEN hour_utc >= ? THEN quota ELSE 0 END), 0) AS week_quota, "+
			"COALESCE(SUM(quota), 0) AS month_quota", dayStart, weekStart).
		Where("user_id = ? AND hour_utc >= ? AND hour_utc < ?", userID, monthStart, cutoff).
		Scan(&sums).Error
	if err != nil {
		topUpBonusHealth.windowQuery.record()
	}
	return sums, err
}

type TopUpBonusUserQuota struct {
	UserID int
	Quota  int64
}

// SumTopUpBonusByUser returns each user's net sum in [start, end).
func SumTopUpBonusByUser(start, end int64) ([]TopUpBonusUserQuota, error) {
	var rows []TopUpBonusUserQuota
	db, cancel := topUpBonusDB()
	defer cancel()
	err := db.Model(&TopUpBonusUsageHourly{}).
		Select("user_id, COALESCE(SUM(quota), 0) AS quota").
		Where("hour_utc >= ? AND hour_utc < ?", start, end).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		topUpBonusHealth.windowQuery.record()
	}
	return rows, err
}

// DeleteTopUpBonusUsageHourlyBefore removes rows older than the retention cutoff.
func DeleteTopUpBonusUsageHourlyBefore(hourUTC int64) error {
	db, cancel := topUpBonusDB()
	defer cancel()
	err := db.Where("hour_utc < ?", hourUTC).Delete(&TopUpBonusUsageHourly{}).Error
	if err != nil {
		topUpBonusHealth.cleanup.record()
	}
	return err
}

// EarliestTopUpBonusHour reports the oldest retained hour, if any.
func EarliestTopUpBonusHour() (int64, bool, error) {
	var earliest sql.NullInt64
	if err := DB.Model(&TopUpBonusUsageHourly{}).Select("MIN(hour_utc)").Row().Scan(&earliest); err != nil {
		return 0, false, err
	}
	return earliest.Int64, earliest.Valid, nil
}

type topUpBonusFailureCounter struct {
	count  atomic.Int64
	lastAt atomic.Int64
}

func (c *topUpBonusFailureCounter) record() {
	c.count.Add(1)
	c.lastAt.Store(time.Now().Unix())
}

func (c *topUpBonusFailureCounter) snapshot() TopUpBonusFailureStat {
	return TopUpBonusFailureStat{Count: c.count.Load(), LastAt: c.lastAt.Load()}
}

// Process-lifetime failure counters; they reset on restart.
var topUpBonusHealth struct {
	hourlyWrite topUpBonusFailureCounter
	windowQuery topUpBonusFailureCounter
	cleanup     topUpBonusFailureCounter
}

type TopUpBonusFailureStat struct {
	Count  int64 `json:"count"`
	LastAt int64 `json:"last_at"`
}

type TopUpBonusHealth struct {
	HourlyWriteFailures TopUpBonusFailureStat `json:"hourly_write_failures"`
	WindowQueryFailures TopUpBonusFailureStat `json:"window_query_failures"`
	CleanupFailures     TopUpBonusFailureStat `json:"cleanup_failures"`
}

func GetTopUpBonusHealth() TopUpBonusHealth {
	return TopUpBonusHealth{
		HourlyWriteFailures: topUpBonusHealth.hourlyWrite.snapshot(),
		WindowQueryFailures: topUpBonusHealth.windowQuery.snapshot(),
		CleanupFailures:     topUpBonusHealth.cleanup.snapshot(),
	}
}
