package model

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 写库失败时 UpdateOption 返回错误且不改内存；读库失败时同步保留内存配置并记录错误。
func TestOptionDatabaseFailuresKeepMemory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "options.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	oldDB, oldName, oldWriter := DB, common.SystemName, gin.DefaultErrorWriter
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{"SystemName": "kept-name"}
	common.OptionMapRWMutex.Unlock()
	var logs bytes.Buffer
	DB, common.SystemName, gin.DefaultErrorWriter = db, "kept-name", &logs
	t.Cleanup(func() {
		DB, common.SystemName, gin.DefaultErrorWriter = oldDB, oldName, oldWriter
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

	loadOptionsFromDatabase()
	assert.Equal(t, "kept-name", common.OptionMap["SystemName"])
	assert.Contains(t, logs.String(), "failed to load options from database")
}
