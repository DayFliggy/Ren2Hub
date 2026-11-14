//go:build known_upstream_restored

package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"path/filepath"
	"testing"
)

func TestRestoredNativeTopUpCountIsCapped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "count.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	old := DB
	DB = db
	t.Cleanup(func() { DB = old; s, _ := db.DB(); s.Close() })
	require.NoError(t, db.AutoMigrate(&TopUp{}))
	rows := make([]TopUp, 10005)
	for i := range rows {
		rows[i] = TopUp{UserId: 71, TradeNo: fmt.Sprintf("cap-%05d", i), CreateTime: common.GetTimestamp()}
	}
	require.NoError(t, db.CreateInBatches(rows, 500).Error)
	result, total, err := ListTopUps(TopUpFilter{UserID: 71}, &common.PageInfo{Page: 1, PageSize: 3})
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.EqualValues(t, 10000, total, "restored_native_topup_count_cap")
}
