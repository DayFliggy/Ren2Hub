package controller

import (
	"bytes"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
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

func restoredStripeWebhook(t *testing.T) func(payload string) int {
	t.Helper()
	restoredCompliance(t)
	oldAPI, oldWebhook, oldPrice := setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = oldAPI, oldWebhook, oldPrice
	})
	setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = "sk_test_local", "whsec_local_test", "price_local"
	router := gin.New()
	router.POST("/stripe", StripeWebhook)
	return func(payload string) int {
		signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: []byte(payload), Secret: setting.StripeWebhookSecret})
		request := httptest.NewRequest(http.MethodPost, "/stripe", bytes.NewReader(signed.Payload))
		request.Header.Set("Stripe-Signature", signed.Header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
}

func restoredStripeEvent(object string) string {
	return `{"id":"evt_local","type":"checkout.session.completed","data":{"object":{"object":"checkout.session","status":"complete","payment_status":"paid",` + object + `}}}`
}

func restoredRejections(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "payment.callback_rejected").Count(&count).Error)
	return count
}
