package model

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 写库失败时 UpdateOption 返回错误且不改内存；读库失败时同步保留内存配置并返回错误。
func TestOptionDatabaseFailuresKeepMemory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "options.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	oldDB, oldName := DB, common.SystemName
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{"SystemName": "kept-name"}
	common.OptionMapRWMutex.Unlock()
	DB, common.SystemName = db, "kept-name"
	t.Cleanup(func() {
		DB, common.SystemName = oldDB, oldName
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	require.Error(t, UpdateOption("SystemName", "unsaved-name"))
	assert.Equal(t, "kept-name", common.SystemName)
	assert.Equal(t, "kept-name", common.OptionMap["SystemName"])

	require.Error(t, LoadOptionsFromDatabase())
	assert.Equal(t, "kept-name", common.OptionMap["SystemName"])
}

// 键不存在时 FirstOrCreate 先插入空值行；随后 Save 失败要整体回滚，否则下一轮同步会把
// 空值读进内存。
func TestUpdateOptionRollsBackCreatedRowWhenSaveFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "options.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	oldDB, oldName := DB, common.SystemName
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	DB, common.SystemName = db, "kept-name"
	t.Cleanup(func() {
		DB, common.SystemName = oldDB, oldName
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
	failure := errors.New("option save unavailable")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("option:save-fail", func(tx *gorm.DB) {
		tx.AddError(failure)
	}))

	require.ErrorIs(t, UpdateOption("SystemName", "unsaved-name"), failure)
	var count int64
	require.NoError(t, db.Model(&Option{}).Where("key = ?", "SystemName").Count(&count).Error)
	assert.Zero(t, count, "the row inserted by FirstOrCreate must roll back with the failed save")
	assert.Equal(t, "kept-name", common.SystemName)
}

func TestUpstreamSyncOptionPartialReadFailureDoesNotPublish(t *testing.T) {
	db := useGmpayTestDB(t)
	oldName := common.SystemName
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{"SystemName": "runtime-kept"}
	common.OptionMapRWMutex.Unlock()
	common.SystemName = "runtime-kept"
	t.Cleanup(func() {
		common.SystemName = oldName
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, db.Create(&Option{Key: "SystemName", Value: "partially-read"}).Error)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("upstream-sync:partial-options", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(errors.New("read failed after partial rows"))
		}
	}))
	require.Error(t, LoadOptionsFromDatabase())
	assert.Equal(t, "runtime-kept", common.SystemName)
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	assert.Equal(t, "runtime-kept", common.OptionMap["SystemName"])
}
