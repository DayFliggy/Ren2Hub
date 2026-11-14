//go:build known_upstream_restored

package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"path/filepath"
	"testing"
)

func restoredCompliance(t *testing.T) {
	p := operation_setting.GetPaymentSetting()
	old := *p
	t.Cleanup(func() { *p = old })
	p.ComplianceConfirmed = true
	p.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}
func restoredFundsDB(t *testing.T) *gorm.DB {
	require.NoError(t, i18n.Init())
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "funds.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	oldDB, oldLog, oldRedis, oldQuota := model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.QuotaPerUnit = 1000
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.AuditLog{}, &model.Option{}, &model.UserSession{}, &model.CasbinRule{}, &model.AuthzRole{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.QuotaPerUnit = oldQuota
		common.SetDatabaseTypes(oldMain, oldLogType)
		sqlDB.Close()
	})
	return db
}
func restoredOrder(t *testing.T, o *model.TopUp) {
	o.Currency = "USD"
	o.PayAmount = "1"
	require.NoError(t, o.Insert())
}
