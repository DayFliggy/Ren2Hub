package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupEpayControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.EpayOrder{}, &model.EpayMerchantVersion{}, &model.User{}, &model.TopUp{}, &model.BillingEvent{}, &model.Log{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousAffiliate := common.RedisEnabled, common.AffiliateEnabled
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.AffiliateEnabled = false, false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.AffiliateEnabled = previousRedis, previousAffiliate
		common.SetDatabaseTypes(previousMainType, previousLogType)
		require.NoError(t, connection.Close())
	})
	options := map[string]string{
		"PayAddress": "https://pay.example.test", "EpayId": "1", "EpayKey": "test-key",
		"Price": "1", "MinTopUp": "1", "PayMethods": `[{"name":"支付宝","type":"alipay"}]`,
		"TopupGroupRatio": `{"default":1}`, "payment_setting.amount_discount": "{}",
		"payment_setting.compliance_confirmed": "true", "payment_setting.compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"general_setting.quota_display_type": "USD", "ServerAddress": "https://shop.example.test",
	}
	for key, value := range options {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "epay-user", Group: "default", Quota: 7, Status: common.UserStatusEnabled}).Error)
	return db
}
