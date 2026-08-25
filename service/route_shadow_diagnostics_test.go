package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRouteShadowDiagnosticsRoundsCoreCoverageUpToThreshold(t *testing.T) {
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

	diagnostics := GetRouteShadowDiagnostics(context.Background())
	assert.False(t, diagnostics.CoverageDataUnavailable)
	assert.Equal(t, int64(3), diagnostics.CoreModelRequestCount)
	assert.Equal(t, 1.0, diagnostics.CoreModelCoverage)
	assert.ElementsMatch(t, []string{"gpt-5", "claude-opus-5"}, diagnostics.CoreModels)
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
