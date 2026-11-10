package controller

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTopUpDiscountControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AuditLog{}, &model.TopUp{}, &model.TopUpDiscountUsageHourly{}, &model.TopUpDiscountConfigVersion{}))
	previousDB, previousLogDB, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	service.ResetTopUpDiscountCaches()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		service.ResetTopUpDiscountCaches()
		require.NoError(t, sqlDB.Close())
	})
	return db
}

// topUpDiscountContractData replaces clock-dependent fields so the committed
// fixtures stay stable; their shapes are what the frontend consumes.
func topUpDiscountContractData(t *testing.T, body map[string]any, fixed map[string]any) map[string]any {
	t.Helper()
	require.Equal(t, true, body["success"], body)
	data := body["data"].(map[string]any)
	for key, value := range fixed {
		_, present := data[key]
		require.True(t, present, key)
		data[key] = value
	}
	return body
}

func TestFrontendContractTopUpDiscount(t *testing.T) {
	db := setupTopUpDiscountControllerTest(t)
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET("/api/option/topup_discount", GetTopUpDiscountConfig)
	engine.PATCH("/api/option/topup_discount", UpdateTopUpDiscountConfig)
	engine.GET("/api/option/topup_discount/status", GetTopUpDiscountStatus)
	engine.POST("/api/option/topup_discount/estimate", EstimateTopUpDiscount)

	loaded := contractRequest(t, engine, http.MethodGet, "/api/option/topup_discount", nil)
	require.Equal(t, http.StatusOK, loaded.Code, loaded.Body.String())
	defaults := decodeContractBody(t, loaded)
	assert.Equal(t, 0.0, defaults["data"].(map[string]any)["version"])
	frontendFixture(t, "topup-discount.get.json", defaults)

	config := model.DefaultTopUpDiscountConfig()
	config.FixedRate = "0.01"
	config.Personal.Month.Mode = model.TopUpDiscountModeOn
	config.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: []model.TopUpDiscountTierConfig{{ThresholdQuota: "50000000", Rate: "0.02"}}}
	patch := frontendFixture(t, "topup-discount.patch.request.json", map[string]any{"expected_version": 0, "config": config})

	rejected := map[string]string{
		"number rate":   `{"expected_version":0,"config":{"schema_version":1,"timezone":"UTC","cap_rate":0.05,"fixed_rate":"0","amount":{"mode":"off","tiers":[]},"site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}}`,
		"unknown field": `{"expected_version":0,"version":3,"config":{}}`,
		"no version":    `{"config":{}}`,
		"no amount":     `{"expected_version":0,"config":{"schema_version":1,"timezone":"UTC","cap_rate":"0.05","fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}}`,
	}
	for name, body := range rejected {
		response := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_discount", []byte(body))
		assert.Equal(t, http.StatusBadRequest, response.Code, name)
		assert.Equal(t, "topup_discount_invalid_config", decodeContractBody(t, response)["code"], name)
	}

	saved := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_discount", patch)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	savedData := decodeContractBody(t, saved)["data"].(map[string]any)
	assert.Equal(t, 1.0, savedData["version"])
	var audits int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "topup_discount.config.update").Count(&audits).Error)
	assert.Equal(t, int64(1), audits)

	conflict := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_discount", patch)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	assert.Equal(t, "topup_discount_version_conflict", decodeContractBody(t, conflict)["code"])

	estimateBody, err := common.Marshal(map[string]any{"config": config})
	require.NoError(t, err)
	estimate := contractRequest(t, engine, http.MethodPost, "/api/option/topup_discount/estimate", estimateBody)
	require.Equal(t, http.StatusOK, estimate.Code, estimate.Body.String())
	frontendFixture(t, "topup-discount-estimate.post.json", topUpDiscountContractData(t, decodeContractBody(t, estimate),
		map[string]any{"from_date": "2026-09-30", "to_date": "2026-10-29"}))
	var versions int64
	require.NoError(t, db.Model(&model.TopUpDiscountConfigVersion{}).Count(&versions).Error)
	assert.Equal(t, int64(1), versions, "an estimate never saves")

	status := contractRequest(t, engine, http.MethodGet, "/api/option/topup_discount/status", nil)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	frontendFixture(t, "topup-discount-status.get.json", topUpDiscountContractData(t, decodeContractBody(t, status),
		map[string]any{"business_date": "2026-10-30", "cutoff_utc": 1793289600, "health": model.TopUpDiscountHealth{
			HourlyWriteFailures: model.TopUpDiscountFailureStat{Count: 2, LastAt: 1793300000},
		}}))

	// A user with $25 in the previous 30 days reaches the first month tier.
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	local := time.Now().In(location)
	yesterday := time.Date(local.Year(), local.Month(), local.Day()-1, 12, 0, 0, 0, location).Unix() / 3600 * 3600
	require.NoError(t, db.Create(&model.TopUpDiscountUsageHourly{UserID: 7001, HourUTC: yesterday, Quota: 12_500_000}).Error)
	self := contractEngine(7001, common.RoleCommonUser)
	self.GET("/api/user/topup-discount", GetSelfTopUpDiscount)
	view := contractRequest(t, self, http.MethodGet, "/api/user/topup-discount", nil)
	require.Equal(t, http.StatusOK, view.Code, view.Body.String())
	selfBody := topUpDiscountContractData(t, decodeContractBody(t, view), map[string]any{"business_date": "2026-10-30"})
	selfData := selfBody["data"].(map[string]any)
	assert.Equal(t, "0.98", selfData["factor"])
	assert.Equal(t, "0.01", selfData["personal_rate"])
	assert.Len(t, selfData["amount_tiers"], 1)
	assert.NotContains(t, view.Body.String(), "site_month_quota")
	frontendFixture(t, "topup-discount-self.get.json", selfBody)
}

// The body limit applies to the whole body: a complete first object followed
// by trailing data past the limit must not be accepted.
func TestTopUpDiscountBodyLimitRejectsTrailingDataPastLimit(t *testing.T) {
	db := setupTopUpDiscountControllerTest(t)
	engine := contractEngine(1, common.RoleRootUser)
	engine.PATCH("/api/option/topup_discount", UpdateTopUpDiscountConfig)
	patch, err := common.Marshal(map[string]any{"expected_version": 0, "config": model.DefaultTopUpDiscountConfig()})
	require.NoError(t, err)

	padded := bytes.Join([][]byte{patch, bytes.Repeat([]byte(" "), topUpDiscountBodyLimit), []byte("{}")}, nil)
	tooLarge := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_discount", padded)
	assert.Equal(t, http.StatusRequestEntityTooLarge, tooLarge.Code)
	assert.Equal(t, "topup_discount_invalid_config", decodeContractBody(t, tooLarge)["code"])

	trailing := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_discount", append(patch, "{}"...))
	assert.Equal(t, http.StatusBadRequest, trailing.Code)

	var versions int64
	require.NoError(t, db.Model(&model.TopUpDiscountConfigVersion{}).Count(&versions).Error)
	assert.Zero(t, versions)
}

// The amount endpoint returns the discounted payable string plus its
// breakdown; a pay request that confirms an outdated price is answered with
// the new quote before any order exists.
func TestFrontendContractTopUpQuote(t *testing.T) {
	db := setupTopUpDiscountControllerTest(t)
	user := model.User{Username: "quote-payer", AffCode: "quote-payer"}
	require.NoError(t, db.Create(&user).Error)
	oldPrice, oldDisplay := operation_setting.Price, operation_setting.GetQuotaDisplayType()
	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	t.Cleanup(func() {
		operation_setting.Price = oldPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
	})
	operation_setting.Price = 7.3
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://epay.example.test", "merchant", "secret"

	config := model.DefaultTopUpDiscountConfig()
	config.CapRate, config.FixedRate = "0.1", "0.01"
	config.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: []model.TopUpDiscountTierConfig{{ThresholdQuota: "50000000", Rate: "0.04"}}}
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveTopUpDiscountConfig(data, 0, 1, time.Now())
	require.NoError(t, err)

	engine := contractEngine(user.Id, common.RoleCommonUser)
	engine.POST("/api/user/amount", RequestAmount)
	engine.POST("/api/user/pay", RequestEpay)
	quoted := contractRequest(t, engine, http.MethodPost, "/api/user/amount", []byte(`{"amount":100,"payment_method":"alipay"}`))
	require.Equal(t, http.StatusOK, quoted.Code)
	body := decodeContractBody(t, quoted)
	require.Equal(t, "success", body["message"], quoted.Body.String())
	assert.Equal(t, "693.50", body["data"], "730 × (1 − 0.04 − 0.01) = 693.5; personal windows are shadow")
	frontendFixture(t, "topup-quote.post.json", body)

	changed := contractRequest(t, engine, http.MethodPost, "/api/user/pay",
		[]byte(`{"amount":100,"payment_method":"alipay","expected_pay_amount":"730.00","expected_currency":"CNY"}`))
	require.Equal(t, http.StatusOK, changed.Code)
	changedBody := decodeContractBody(t, changed)
	assert.Equal(t, "topup_quote_changed", changedBody["code"])
	frontendFixture(t, "topup-quote-changed.post.json", changedBody)
	var orders int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders)

	malformed := contractRequest(t, engine, http.MethodPost, "/api/user/pay",
		[]byte(`{"amount":100,"payment_method":"alipay","expected_pay_amount":"693.5"}`))
	assert.Equal(t, "error", decodeContractBody(t, malformed)["message"], "a half confirmation is rejected")
}

func TestTopUpDiscountUnavailableCarriesCodeWithoutCause(t *testing.T) {
	db := setupTopUpDiscountControllerTest(t)
	// User 1 is excluded by default and would never read usage.
	user := model.User{Id: 7, Username: "unavailable", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "unavailable"}
	require.NoError(t, db.Create(&user).Error)
	oldPrice, oldDisplay := operation_setting.Price, operation_setting.GetQuotaDisplayType()
	oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	t.Cleanup(func() {
		operation_setting.Price = oldPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
	})
	operation_setting.Price = 7.3
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://epay.example.test", "merchant", "secret"
	config := model.DefaultTopUpDiscountConfig()
	config.Site = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: []model.TopUpDiscountTierConfig{{ThresholdQuota: "1000", Rate: "0.01"}}}
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveTopUpDiscountConfig(data, 0, 1, time.Now())
	require.NoError(t, err)
	// A live site table that cannot be read must block pricing.
	require.NoError(t, db.Migrator().DropTable(&model.TopUpDiscountUsageHourly{}))

	engine := contractEngine(user.Id, common.RoleCommonUser)
	engine.POST("/api/user/amount", RequestAmount)
	engine.POST("/api/user/pay", RequestEpay)
	engine.GET("/api/user/topup-discount", GetSelfTopUpDiscount)
	for path, payload := range map[string]string{
		"/api/user/amount": `{"amount":100,"payment_method":"alipay"}`,
		"/api/user/pay":    `{"amount":100,"payment_method":"alipay","expected_pay_amount":"730.00","expected_currency":"CNY"}`,
	} {
		body := decodeContractBody(t, contractRequest(t, engine, http.MethodPost, path, []byte(payload)))
		assert.Equal(t, "error", body["message"], path)
		assert.Equal(t, "topup_discount_unavailable", body["code"], path)
		assert.Equal(t, "Top-up discount is temporarily unavailable, please try again", body["message_key"], path)
	}
	var orders int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders, "an unreadable live table creates no order")

	self := contractRequest(t, engine, http.MethodGet, "/api/user/topup-discount", nil)
	assert.Equal(t, http.StatusServiceUnavailable, self.Code)
	selfBody := decodeContractBody(t, self)
	assert.Equal(t, "topup_discount_unavailable", selfBody["code"])
	assert.NotContains(t, self.Body.String(), "topup_discount_usage_hourly", "the database cause stays in the server log")
}
