package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSystemUsageIsNullUnlessMonitorSampleIsFresh(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	enabled := common.PerformanceMonitorConfig{Enabled: true}
	fresh := common.SystemStatus{CPUUsage: 0, MemoryUsage: 41.5, SampledAt: now.Add(-5 * time.Second)}
	require.Nil(t, systemUsageFrom(common.PerformanceMonitorConfig{}, fresh, now), "monitor disabled")
	require.Nil(t, systemUsageFrom(enabled, common.SystemStatus{}, now), "no sample yet")
	require.Nil(t, systemUsageFrom(enabled, common.SystemStatus{CPUUsage: 9, SampledAt: now.Add(-31 * time.Second)}, now), "stale sample")
	usage := systemUsageFrom(enabled, fresh, now)
	require.NotNil(t, usage)
	require.Equal(t, SystemUsage{CPUPercent: 0, MemoryPercent: 41.5, SampledAt: fresh.SampledAt.Unix()}, *usage)
}

func TestPerformanceStatsReportsPoolAndNullUsageWhenMonitorOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(7)
	previous := common.GetPerformanceMonitorConfig()
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: false})
	t.Cleanup(func() { common.SetPerformanceMonitorConfig(previous) })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/performance/stats", nil)
	GetPerformanceStats(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var body struct {
		Data map[string]common.RawMessage `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "null", string(body.Data["system_usage"]))
	var pool DBPoolStats
	require.NoError(t, common.Unmarshal(body.Data["db_pool"], &pool))
	require.NotNil(t, pool.Main)
	require.Equal(t, 7, pool.Main.MaxOpenConnections)
	require.True(t, pool.LogShared)
	require.Nil(t, pool.Log)
	// 原生字段保持不变。
	for _, key := range []string{"cache_stats", "memory_stats", "disk_cache_info", "disk_space_info", "config"} {
		require.Contains(t, body.Data, key)
	}
}
