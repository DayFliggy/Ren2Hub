package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRouteShadowDiagnosticsRoundsCoreCoverageUpToThreshold(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	defer func() { _ = client.FlushDB(context.Background()).Err() }()
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
		_ = client.Close()
	})
	originalLogDB := model.LOG_DB
	originalLogType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = db
	common.SetDatabaseTypes(common.MainDatabaseType(), common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(common.MainDatabaseType(), originalLogType)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	now := time.Now().Unix()
	require.NoError(t, db.Create([]model.Log{
		{CreatedAt: now, Type: model.LogTypeConsume, ModelName: "gpt-5"},
		{CreatedAt: now, Type: model.LogTypeConsume, ModelName: "gpt-5"},
		{CreatedAt: now, Type: model.LogTypeConsume, ModelName: "claude-opus-5"},
	}).Error)
	recordRouteShadowAggregate(RouteShadowDecision{NormalizedRequestModel: "gpt-5", LabSlug: "openai"})
	recordRouteShadowAggregate(RouteShadowDecision{NormalizedRequestModel: "claude-opus-5", LabSlug: "anthropic"})

	diagnostics := GetRouteShadowDiagnostics(context.Background())
	assert.False(t, diagnostics.CoverageDataUnavailable)
	assert.False(t, diagnostics.ShadowDataUnavailable)
	assert.Equal(t, "redis_hourly_aggregate", diagnostics.ShadowDataSource)
	assert.Equal(t, int64(3), diagnostics.CoreModelRequestCount)
	assert.Equal(t, 1.0, diagnostics.CoreModelCoverage)
	assert.ElementsMatch(t, []string{"gpt-5", "claude-opus-5"}, diagnostics.CoreModels)
	assert.Equal(t, 1.0, diagnostics.CoreModelLabResolution)
}

func TestRouteShadowDiagnosticsMarksEmptyLogWindowUnavailable(t *testing.T) {
	originalLogDB := model.LOG_DB
	originalLogType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = db
	common.SetDatabaseTypes(common.MainDatabaseType(), common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(common.MainDatabaseType(), originalLogType)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	diagnostics := GetRouteShadowDiagnostics(context.Background())
	assert.True(t, diagnostics.CoverageDataUnavailable)
	assert.Zero(t, diagnostics.CoreModelRequestCount)
	assert.Zero(t, diagnostics.CoreModelCoverage)
}

func TestRouteShadowDiagnosticsDoesNotUseProcessLocalResolutionAsSevenDayEvidence(t *testing.T) {
	originalLogDB := model.LOG_DB
	originalLogType := common.LogDatabaseType()
	originalRedisEnabled, originalRDB := common.RedisEnabled, common.RDB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = db
	common.RedisEnabled, common.RDB = false, nil
	common.SetDatabaseTypes(common.MainDatabaseType(), common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
		common.RedisEnabled, common.RDB = originalRedisEnabled, originalRDB
		common.SetDatabaseTypes(common.MainDatabaseType(), originalLogType)
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&model.Log{CreatedAt: time.Now().Unix(), Type: model.LogTypeConsume, ModelName: "gpt-5"}).Error)
	observeShadowDecision(RouteShadowDecision{NormalizedRequestModel: "gpt-5", LabSlug: "openai"})

	diagnostics := GetRouteShadowDiagnostics(context.Background())
	assert.False(t, diagnostics.CoverageDataUnavailable)
	assert.True(t, diagnostics.ShadowDataUnavailable)
	assert.Zero(t, diagnostics.CoreModelLabResolution)
}

func TestObserveShadowDecisionCountsResolvedMixedCapability(t *testing.T) {
	const normalizedModel = "shadow-mixed-resolution-metric"

	before := routeShadowModelMetrics()[normalizedModel]
	observeShadowDecision(RouteShadowDecision{
		NormalizedRequestModel: normalizedModel,
		LabSlug:                "openai",
		HasMixed:               true,
	})
	after := routeShadowModelMetrics()[normalizedModel]

	assert.Equal(t, before.Decisions+1, after.Decisions)
	assert.Equal(t, before.Resolved+1, after.Resolved)
}
