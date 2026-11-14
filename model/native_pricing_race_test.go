//go:build known_upstream_race

package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
	"path/filepath"
	"sync"
	"testing"
)

func TestRestoredNativeControlPricingConcurrentRefresh(t *testing.T) {
	db := restoredPricingRaceDB(t, &Channel{}, &Ability{}, &Model{}, &Vendor{})
	require.NoError(t, db.Create(&Channel{Id: 79, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "fixture-key"}).Error)
	require.NoError(t, db.Create(&Ability{ChannelId: 79, Enabled: true, Model: "fixture-race"}).Error)
	InvalidatePricingCache()
	_ = GetPricing()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = GetPricing()
			}
		}()
	}
	for range 20 {
		RefreshPricing()
		InvalidatePricingCache()
	}
	wg.Wait()
}

func restoredPricingRaceDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pricing-race.db")), &gorm.Config{Logger: glog.Default.LogMode(glog.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(models...))
	oldDB, oldLog, oldMain, oldLogType, oldMap, oldMemory := DB, LOG_DB, common.MainDatabaseType(), common.LogDatabaseType(), common.OptionMap, common.MemoryCacheEnabled
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.OptionMap = map[string]string{}
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMain, oldLogType)
		common.OptionMap, common.MemoryCacheEnabled = oldMap, oldMemory
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return db
}
