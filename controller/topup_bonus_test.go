package controller

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTopUpBonusControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AuditLog{}, &model.TopUp{}, &model.Redemption{}, &model.AdminQuotaOperation{},
		&model.TopUpBonusUsageHourly{}, &model.TopUpBonusConfigVersion{}, &model.TopUpBonusDecisionRecord{}))
	previousDB, previousLogDB, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	service.ResetTopUpBonusCaches()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		service.ResetTopUpBonusCaches()
		require.NoError(t, sqlDB.Close())
	})
	return db
}

// topUpBonusContractData replaces clock-dependent fields so the committed
// fixtures stay stable; their shapes are what the frontend consumes.
func topUpBonusContractData(t *testing.T, body map[string]any, fixed map[string]any) map[string]any {
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

func TestFrontendContractTopUpBonus(t *testing.T) {
	db := setupTopUpBonusControllerTest(t)
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET("/api/option/topup_bonus", GetTopUpBonusConfig)
	engine.PATCH("/api/option/topup_bonus", UpdateTopUpBonusConfig)
	engine.GET("/api/option/topup_bonus/status", GetTopUpBonusStatus)
	engine.POST("/api/option/topup_bonus/estimate", EstimateTopUpBonus)

	loaded := contractRequest(t, engine, http.MethodGet, "/api/option/topup_bonus", nil)
	require.Equal(t, http.StatusOK, loaded.Code, loaded.Body.String())
	defaults := decodeContractBody(t, loaded)
	assert.Equal(t, 0.0, defaults["data"].(map[string]any)["version"])
	frontendFixture(t, "topup-bonus.get.json", defaults)

	config := model.DefaultTopUpBonusConfig()
	config.FixedRate = "0.01"
	config.Personal.Month.Mode = model.TopUpBonusModeOn
	config.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: []model.TopUpBonusTierConfig{{ThresholdQuota: "50000000", Rate: "0.02"}}}
	patch := frontendFixture(t, "topup-bonus.patch.request.json", map[string]any{"expected_version": 0, "config": config})

	rejected := map[string]string{
		"number rate":   `{"expected_version":0,"config":{"schema_version":1,"timezone":"UTC","cap_rate":0.05,"fixed_rate":"0","amount":{"mode":"off","tiers":[]},"site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}}`,
		"unknown field": `{"expected_version":0,"version":3,"config":{}}`,
		"no version":    `{"config":{}}`,
		"no amount":     `{"expected_version":0,"config":{"schema_version":1,"timezone":"UTC","cap_rate":"0.05","fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}}`,
	}
	for name, body := range rejected {
		response := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_bonus", []byte(body))
		assert.Equal(t, http.StatusBadRequest, response.Code, name)
		assert.Equal(t, "topup_bonus_invalid_config", decodeContractBody(t, response)["code"], name)
	}

	saved := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_bonus", patch)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	savedData := decodeContractBody(t, saved)["data"].(map[string]any)
	assert.Equal(t, 1.0, savedData["version"])
	var audits int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "topup_bonus.config.update").Count(&audits).Error)
	assert.Equal(t, int64(1), audits)

	conflict := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_bonus", patch)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	assert.Equal(t, "topup_bonus_version_conflict", decodeContractBody(t, conflict)["code"])

	estimateBody, err := common.Marshal(map[string]any{"config": config})
	require.NoError(t, err)
	estimate := contractRequest(t, engine, http.MethodPost, "/api/option/topup_bonus/estimate", estimateBody)
	require.Equal(t, http.StatusOK, estimate.Code, estimate.Body.String())
	frontendFixture(t, "topup-bonus-estimate.post.json", topUpBonusContractData(t, decodeContractBody(t, estimate),
		map[string]any{"from_date": "2026-09-30", "to_date": "2026-10-29"}))
	var versions int64
	require.NoError(t, db.Model(&model.TopUpBonusConfigVersion{}).Count(&versions).Error)
	assert.Equal(t, int64(1), versions, "an estimate never saves")

	status := contractRequest(t, engine, http.MethodGet, "/api/option/topup_bonus/status", nil)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	frontendFixture(t, "topup-bonus-status.get.json", topUpBonusContractData(t, decodeContractBody(t, status),
		map[string]any{"business_date": "2026-10-30", "cutoff_utc": 1793289600, "health": model.TopUpBonusHealth{
			HourlyWriteFailures: model.TopUpBonusFailureStat{Count: 2, LastAt: 1793300000},
		}}))

	// A user with $25 in the previous 30 days reaches the first month tier.
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	local := time.Now().In(location)
	yesterday := time.Date(local.Year(), local.Month(), local.Day()-1, 12, 0, 0, 0, location).Unix() / 3600 * 3600
	require.NoError(t, db.Create(&model.TopUpBonusUsageHourly{UserID: 7001, HourUTC: yesterday, Quota: 12_500_000}).Error)
	self := contractEngine(7001, common.RoleCommonUser)
	self.GET("/api/user/topup-bonus", GetSelfTopUpBonus)
	view := contractRequest(t, self, http.MethodGet, "/api/user/topup-bonus", nil)
	require.Equal(t, http.StatusOK, view.Code, view.Body.String())
	selfBody := topUpBonusContractData(t, decodeContractBody(t, view), map[string]any{"business_date": "2026-10-30"})
	selfData := selfBody["data"].(map[string]any)
	assert.NotContains(t, selfData, "factor", "a bonus never changes the price")
	assert.Equal(t, "0.01", selfData["personal_rate"])
	assert.Equal(t, "0.02", selfData["applied_rate"], "fixed 1% + month 1%; the amount tier needs a principal")
	assert.Len(t, selfData["amount_tiers"], 1)
	assert.NotContains(t, view.Body.String(), "site_month_quota")
	frontendFixture(t, "topup-bonus-self.get.json", selfBody)
}

// The body limit applies to the whole body: a complete first object followed
// by trailing data past the limit must not be accepted.
func TestTopUpBonusBodyLimitRejectsTrailingDataPastLimit(t *testing.T) {
	db := setupTopUpBonusControllerTest(t)
	engine := contractEngine(1, common.RoleRootUser)
	engine.PATCH("/api/option/topup_bonus", UpdateTopUpBonusConfig)
	patch, err := common.Marshal(map[string]any{"expected_version": 0, "config": model.DefaultTopUpBonusConfig()})
	require.NoError(t, err)

	padded := bytes.Join([][]byte{patch, bytes.Repeat([]byte(" "), topUpBonusBodyLimit), []byte("{}")}, nil)
	tooLarge := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_bonus", padded)
	assert.Equal(t, http.StatusRequestEntityTooLarge, tooLarge.Code)
	assert.Equal(t, "topup_bonus_invalid_config", decodeContractBody(t, tooLarge)["code"])

	trailing := contractRequest(t, engine, http.MethodPatch, "/api/option/topup_bonus", append(patch, "{}"...))
	assert.Equal(t, http.StatusBadRequest, trailing.Code)

	var versions int64
	require.NoError(t, db.Model(&model.TopUpBonusConfigVersion{}).Count(&versions).Error)
	assert.Zero(t, versions)
}

// The amount endpoint returns the unchanged payable string plus the bonus
// breakdown; a pay request that confirms an outdated bonus is answered with
// the new quote before any order exists.
func TestFrontendContractTopUpQuote(t *testing.T) {
	db := setupTopUpBonusControllerTest(t)
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

	config := model.DefaultTopUpBonusConfig()
	config.CapRate, config.FixedRate = "0.1", "0.01"
	config.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: []model.TopUpBonusTierConfig{{ThresholdQuota: "50000000", Rate: "0.04"}}}
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveTopUpBonusConfig(data, 0, 1, time.Now())
	require.NoError(t, err)

	engine := contractEngine(user.Id, common.RoleCommonUser)
	engine.POST("/api/user/amount", RequestAmount)
	engine.POST("/api/user/pay", RequestEpay)
	quoted := contractRequest(t, engine, http.MethodPost, "/api/user/amount", []byte(`{"amount":100,"payment_method":"alipay"}`))
	require.Equal(t, http.StatusOK, quoted.Code)
	body := decodeContractBody(t, quoted)
	require.Equal(t, "success", body["message"], quoted.Body.String())
	assert.Equal(t, "730.00", body["data"], "the bonus never changes the price")
	quote := body["quote"].(map[string]any)
	assert.Equal(t, 50_000_000.0, quote["principal_quota"])
	assert.Equal(t, 2_500_000.0, quote["bonus_quota"], "50,000,000 × (0.04 + 0.01); personal windows are shadow")
	assert.Equal(t, 52_500_000.0, quote["expected_quota"])
	frontendFixture(t, "topup-quote.post.json", body)

	changed := contractRequest(t, engine, http.MethodPost, "/api/user/pay",
		[]byte(`{"amount":100,"payment_method":"alipay","expected_pay_amount":"730.00","expected_currency":"CNY","expected_bonus_quota":0}`))
	require.Equal(t, http.StatusOK, changed.Code)
	changedBody := decodeContractBody(t, changed)
	assert.Equal(t, "topup_quote_changed", changedBody["code"])
	frontendFixture(t, "topup-quote-changed.post.json", changedBody)
	var orders int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders)

	malformed := contractRequest(t, engine, http.MethodPost, "/api/user/pay",
		[]byte(`{"amount":100,"payment_method":"alipay","expected_pay_amount":"730.00"}`))
	assert.Equal(t, "error", decodeContractBody(t, malformed)["message"], "a half confirmation is rejected")
	for _, bonus := range []string{`null`, `"2500000"`, `-1`, `2500000.0`, `25e5`, `9007199254740992`} {
		invalid := contractRequest(t, engine, http.MethodPost, "/api/user/pay",
			[]byte(`{"amount":100,"payment_method":"alipay","expected_bonus_quota":`+bonus+`}`))
		invalidBody := decodeContractBody(t, invalid)
		assert.Equal(t, "error", invalidBody["message"], bonus)
		assert.NotContains(t, invalidBody, "code", bonus)
	}
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders)

	self := contractEngine(user.Id, common.RoleCommonUser)
	self.GET("/api/user/topup-bonus/preview", GetTopUpBonusPreview)
	preview := contractRequest(t, self, http.MethodGet, "/api/user/topup-bonus/preview?principal_quota=50000000", nil)
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	previewBody := decodeContractBody(t, preview)
	assert.Equal(t, 2_500_000.0, previewBody["data"].(map[string]any)["bonus_quota"])
	frontendFixture(t, "topup-bonus-preview.get.json", previewBody)
	for _, principal := range []string{"", "0", "-1", "abc", "1.5", "9007199254740992"} {
		invalid := contractRequest(t, self, http.MethodGet, "/api/user/topup-bonus/preview?principal_quota="+principal, nil)
		assert.Equal(t, http.StatusBadRequest, invalid.Code, principal)
	}
}

// Stripe and Creem charge the provider's price, so their confirmation carries
// only the bonus; a changed bonus is answered before any session or order.
func TestRemotelyPricedTopUpRejectsChangedBonus(t *testing.T) {
	db := setupTopUpBonusControllerTest(t)
	user := model.User{Username: "remote-payer", AffCode: "remote-payer"}
	require.NoError(t, db.Create(&user).Error)
	oldProducts := setting.CreemProducts
	t.Cleanup(func() { setting.CreemProducts = oldProducts })
	setting.CreemProducts = `[{"productId":"prod_bonus","name":"Bonus","price":10,"currency":"USD","quota":1000000}]`
	config := model.DefaultTopUpBonusConfig()
	config.CapRate, config.FixedRate = "0.1", "0.02"
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveTopUpBonusConfig(data, 0, 1, time.Now())
	require.NoError(t, err)

	engine := contractEngine(user.Id, common.RoleCommonUser)
	engine.POST("/api/user/stripe/pay", RequestStripePay)
	engine.POST("/api/user/creem/pay", RequestCreemPay)
	for path, payload := range map[string]string{
		"/api/user/stripe/pay": `{"amount":2,"payment_method":"stripe","expected_bonus_quota":0}`,
		"/api/user/creem/pay":  `{"product_id":"prod_bonus","payment_method":"creem","expected_bonus_quota":0}`,
	} {
		body := decodeContractBody(t, contractRequest(t, engine, http.MethodPost, path, []byte(payload)))
		assert.Equal(t, "topup_bonus_changed", body["code"], path)
		assert.Equal(t, "The top-up bonus changed, please confirm the new bonus", body["message_key"], path)
		bonus := body["bonus"].(map[string]any)
		assert.Equal(t, 20_000.0, bonus["bonus_quota"], path)
		assert.Equal(t, 1_020_000.0, bonus["expected_quota"], path)
		assert.NotContains(t, bonus, "pay_amount", "the provider decides the price")
	}
	var orders int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders)
}

func TestTopUpBonusUnavailableCarriesCodeWithoutCause(t *testing.T) {
	db := setupTopUpBonusControllerTest(t)
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
	config := model.DefaultTopUpBonusConfig()
	config.Site = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: []model.TopUpBonusTierConfig{{ThresholdQuota: "1000", Rate: "0.01"}}}
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveTopUpBonusConfig(data, 0, 1, time.Now())
	require.NoError(t, err)
	// A live site table that cannot be read must block pricing.
	require.NoError(t, db.Migrator().DropTable(&model.TopUpBonusUsageHourly{}))

	engine := contractEngine(user.Id, common.RoleCommonUser)
	engine.POST("/api/user/amount", RequestAmount)
	engine.POST("/api/user/pay", RequestEpay)
	engine.GET("/api/user/topup-bonus", GetSelfTopUpBonus)
	engine.GET("/api/user/topup-bonus/preview", GetTopUpBonusPreview)
	for path, payload := range map[string]string{
		"/api/user/amount": `{"amount":100,"payment_method":"alipay"}`,
		"/api/user/pay":    `{"amount":100,"payment_method":"alipay","expected_pay_amount":"730.00","expected_currency":"CNY"}`,
	} {
		body := decodeContractBody(t, contractRequest(t, engine, http.MethodPost, path, []byte(payload)))
		assert.Equal(t, "error", body["message"], path)
		assert.Equal(t, "topup_bonus_unavailable", body["code"], path)
		assert.Equal(t, "Top-up bonus is temporarily unavailable, please try again", body["message_key"], path)
	}
	var orders int64
	require.NoError(t, db.Model(&model.TopUp{}).Count(&orders).Error)
	assert.Zero(t, orders, "an unreadable live table creates no order")

	for _, path := range []string{"/api/user/topup-bonus", "/api/user/topup-bonus/preview?principal_quota=1000"} {
		self := contractRequest(t, engine, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusServiceUnavailable, self.Code, path)
		selfBody := decodeContractBody(t, self)
		assert.Equal(t, "topup_bonus_unavailable", selfBody["code"], path)
		assert.NotContains(t, self.Body.String(), "topup_bonus_usage_hourly", "the database cause stays in the server log")
	}
}
