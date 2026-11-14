package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type currencyRuntime struct {
	usdRate, customRate, quotaPerUnit float64
}

func readCurrencyRuntime() currencyRuntime {
	return currencyRuntime{
		usdRate:      operation_setting.USDExchangeRate,
		customRate:   operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate,
		quotaPerUnit: common.QuotaPerUnit,
	}
}

// setupCurrencyOptions stores valid currency options in a fresh database and
// restores the global option state afterwards.
func setupCurrencyOptions(t *testing.T) map[string]string {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "options.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	oldDB, oldRuntime := DB, readCurrencyRuntime()
	common.OptionMapRWMutex.Lock()
	oldMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	DB = db
	t.Cleanup(func() {
		DB = oldDB
		operation_setting.USDExchangeRate = oldRuntime.usdRate
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = oldRuntime.customRate
		common.QuotaPerUnit = oldRuntime.quotaPerUnit
		common.OptionMapRWMutex.Lock()
		common.OptionMap = oldMap
		common.OptionMapRWMutex.Unlock()
	})
	stored := map[string]string{
		"USDExchangeRate": "7.3",
		"general_setting.custom_currency_exchange_rate": "2",
		"QuotaPerUnit": "500000",
		"SystemName":   "kept-name",
	}
	for key, value := range stored {
		require.NoError(t, UpdateOption(key, value))
	}
	return stored
}

func TestUpdateOptionAcceptsPositiveCurrencyOptions(t *testing.T) {
	setupCurrencyOptions(t)
	require.NoError(t, UpdateOption("USDExchangeRate", "6.9"))
	require.NoError(t, UpdateOptionsBulk(map[string]string{
		"general_setting.custom_currency_exchange_rate": "0.5",
		"QuotaPerUnit": "1000",
	}))
	assert.Equal(t, currencyRuntime{usdRate: 6.9, customRate: 0.5, quotaPerUnit: 1000}, readCurrencyRuntime())
	var row Option
	require.NoError(t, DB.First(&row, "key = ?", "QuotaPerUnit").Error)
	assert.Equal(t, "1000", row.Value)
}
