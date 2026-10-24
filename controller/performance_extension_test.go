package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSystemUsageUsesTheSameObservationSnapshot(t *testing.T) {
	cpu, memory, sampledAt := 0.0, 41.5, int64(1792583995)
	observation := service.PerformanceObservation{Resources: service.ObservationResources{
		CPU:    service.ObservationResource{Available: true, Percent: &cpu, SampledAt: &sampledAt},
		Memory: service.ObservationResource{Available: true, Percent: &memory, SampledAt: &sampledAt},
	}}
	require.Nil(t, systemUsageFrom(service.PerformanceObservation{}), "no sample yet")
	usage := systemUsageFrom(observation)
	require.NotNil(t, usage)
	require.Equal(t, SystemUsage{CPUPercent: cpu, MemoryPercent: memory, SampledAt: sampledAt}, *usage)
	observation.Stale = true
	require.Nil(t, systemUsageFrom(observation), "stale snapshot")
	observation.Stale = false
	observation.Resources.Memory.Available = false
	require.Nil(t, systemUsageFrom(observation), "one unavailable resource")
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
	var observation service.PerformanceObservation
	require.NoError(t, common.Unmarshal(body.Data["observability"], &observation))
	require.Equal(t, "1h", observation.Series.Window)
}

func TestPerformanceStatsRejectsInvalidObservationWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/performance/stats?window=bad", nil)
	GetPerformanceStats(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "observability")
}
