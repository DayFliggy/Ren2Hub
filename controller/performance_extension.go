package controller

import (
	"database/sql"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

// PerformanceStatsExtension 是 /api/performance/stats 的本地追加字段，嵌入原生结构体。
type PerformanceStatsExtension struct {
	// SystemUsage 为性能监控的最近一次采样；监控关闭、尚未采样或采样过期时为 null，不返回 0。
	SystemUsage *SystemUsage `json:"system_usage"`
	// DBPool 来自 sql.DB.Stats()。
	DBPool        DBPoolStats                    `json:"db_pool"`
	Observability service.PerformanceObservation `json:"observability"`
}

// SystemUsage 与哨兵使用同一份采样：优先容器 cgroup 口径，不可用时为宿主机口径。
type SystemUsage struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	SampledAt     int64   `json:"sampled_at"`
}

// DBPoolStats 中 Log 为 null 且 LogShared 为 true 表示日志库与主库共用连接池。
type DBPoolStats struct {
	Main      *DBPoolStat `json:"main"`
	Log       *DBPoolStat `json:"log"`
	LogShared bool        `json:"log_shared"`
}

type DBPoolStat struct {
	MaxOpenConnections int   `json:"max_open_connections"`
	OpenConnections    int   `json:"open_connections"`
	InUse              int   `json:"in_use"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDurationMs     int64 `json:"wait_duration_ms"`
	MaxIdleClosed      int64 `json:"max_idle_closed"`
	MaxLifetimeClosed  int64 `json:"max_lifetime_closed"`
}

func newPerformanceStatsExtension(observation service.PerformanceObservation) PerformanceStatsExtension {
	return PerformanceStatsExtension{
		SystemUsage:   systemUsageFrom(observation),
		DBPool:        dbPoolStats(),
		Observability: observation,
	}
}

func systemUsageFrom(observation service.PerformanceObservation) *SystemUsage {
	cpu, memory := observation.Resources.CPU, observation.Resources.Memory
	if observation.Stale || !cpu.Available || !memory.Available || cpu.Percent == nil || memory.Percent == nil || cpu.SampledAt == nil {
		return nil
	}
	return &SystemUsage{CPUPercent: *cpu.Percent, MemoryPercent: *memory.Percent, SampledAt: *cpu.SampledAt}
}

func dbPoolStats() DBPoolStats {
	result := DBPoolStats{Main: dbPoolStat("main", model.DB)}
	if model.LOG_DB == nil || model.LOG_DB == model.DB {
		result.LogShared = model.LOG_DB != nil
		return result
	}
	result.Log = dbPoolStat("log", model.LOG_DB)
	return result
}

// dbPoolStat 读取失败时返回 null 并记录错误，不伪造 0 连接。
func dbPoolStat(name string, db *gorm.DB) *DBPoolStat {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		common.SysError(common.LogText("failed to read %s database pool stats: %s", name, err.Error()))
		return nil
	}
	return newDBPoolStat(sqlDB.Stats())
}

func newDBPoolStat(stats sql.DBStats) *DBPoolStat {
	return &DBPoolStat{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDurationMs:     stats.WaitDuration.Milliseconds(),
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	}
}
