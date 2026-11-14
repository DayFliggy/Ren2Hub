package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 写库失败时 UpdateOption 返回错误且不改内存；读库失败时同步保留内存配置并返回错误。

// 键不存在时 FirstOrCreate 先插入空值行；随后 Save 失败要整体回滚，否则下一轮同步会把
// 空值读进内存。

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
