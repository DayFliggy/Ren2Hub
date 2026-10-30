package controller

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUsageDiscountControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AuditLog{}, &model.UsageDiscountHourly{}, &model.UsageDiscountConfigVersion{}))
	previousDB, previousLogDB, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	service.ResetUsageDiscountCaches()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogDB, previousRedis
		service.ResetUsageDiscountCaches()
		require.NoError(t, sqlDB.Close())
	})
	return db
}

// usageDiscountContractData replaces clock-dependent fields so the committed
// fixtures stay stable; their shapes are what the frontend consumes.
func usageDiscountContractData(t *testing.T, body map[string]any, fixed map[string]any) map[string]any {
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

func TestFrontendContractUsageDiscount(t *testing.T) {
	db := setupUsageDiscountControllerTest(t)
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET("/api/option/usage_discount", GetUsageDiscountConfig)
	engine.PATCH("/api/option/usage_discount", UpdateUsageDiscountConfig)
	engine.GET("/api/option/usage_discount/status", GetUsageDiscountStatus)
	engine.POST("/api/option/usage_discount/estimate", EstimateUsageDiscount)

	loaded := contractRequest(t, engine, http.MethodGet, "/api/option/usage_discount", nil)
	require.Equal(t, http.StatusOK, loaded.Code, loaded.Body.String())
	defaults := decodeContractBody(t, loaded)
	assert.Equal(t, 0.0, defaults["data"].(map[string]any)["version"])
	frontendFixture(t, "usage-discount.get.json", defaults)

	config := model.DefaultUsageDiscountConfig()
	config.FixedRate = "0.01"
	config.Personal.Month.Mode = model.UsageDiscountModeOn
	patch := frontendFixture(t, "usage-discount.patch.request.json", map[string]any{"expected_version": 0, "config": config})

	rejected := map[string]string{
		"number rate":   `{"expected_version":0,"config":{"schema_version":1,"timezone":"UTC","cap_rate":0.05,"fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}}`,
		"unknown field": `{"expected_version":0,"version":3,"config":{}}`,
		"no version":    `{"config":{}}`,
	}
	for name, body := range rejected {
		response := contractRequest(t, engine, http.MethodPatch, "/api/option/usage_discount", []byte(body))
		assert.Equal(t, http.StatusBadRequest, response.Code, name)
		assert.Equal(t, "usage_discount_invalid_config", decodeContractBody(t, response)["code"], name)
	}

	saved := contractRequest(t, engine, http.MethodPatch, "/api/option/usage_discount", patch)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	savedData := decodeContractBody(t, saved)["data"].(map[string]any)
	assert.Equal(t, 1.0, savedData["version"])
	var audits int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "usage_discount.config.update").Count(&audits).Error)
	assert.Equal(t, int64(1), audits)

	conflict := contractRequest(t, engine, http.MethodPatch, "/api/option/usage_discount", patch)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	assert.Equal(t, "usage_discount_version_conflict", decodeContractBody(t, conflict)["code"])

	estimateBody, err := common.Marshal(map[string]any{"config": config})
	require.NoError(t, err)
	estimate := contractRequest(t, engine, http.MethodPost, "/api/option/usage_discount/estimate", estimateBody)
	require.Equal(t, http.StatusOK, estimate.Code, estimate.Body.String())
	frontendFixture(t, "usage-discount-estimate.post.json", usageDiscountContractData(t, decodeContractBody(t, estimate),
		map[string]any{"from_date": "2026-09-30", "to_date": "2026-10-29"}))
	var versions int64
	require.NoError(t, db.Model(&model.UsageDiscountConfigVersion{}).Count(&versions).Error)
	assert.Equal(t, int64(1), versions, "an estimate never saves")

	status := contractRequest(t, engine, http.MethodGet, "/api/option/usage_discount/status", nil)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	frontendFixture(t, "usage-discount-status.get.json", usageDiscountContractData(t, decodeContractBody(t, status),
		map[string]any{"business_date": "2026-10-30", "cutoff_utc": 1793289600, "health": model.UsageDiscountHealth{
			HourlyWriteFailures: model.UsageDiscountFailureStat{Count: 2, LastAt: 1793300000},
		}}))

	// A user with $25 in the previous 30 days reaches the first month tier.
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	local := time.Now().In(location)
	yesterday := time.Date(local.Year(), local.Month(), local.Day()-1, 12, 0, 0, 0, location).Unix() / 3600 * 3600
	require.NoError(t, db.Create(&model.UsageDiscountHourly{UserID: 7001, HourUTC: yesterday, Quota: 12_500_000}).Error)
	self := contractEngine(7001, common.RoleCommonUser)
	self.GET("/api/user/usage-discount", GetSelfUsageDiscount)
	view := contractRequest(t, self, http.MethodGet, "/api/user/usage-discount", nil)
	require.Equal(t, http.StatusOK, view.Code, view.Body.String())
	selfBody := usageDiscountContractData(t, decodeContractBody(t, view), map[string]any{"business_date": "2026-10-30"})
	selfData := selfBody["data"].(map[string]any)
	assert.Equal(t, "0.98", selfData["factor"])
	assert.NotContains(t, view.Body.String(), "site_month_quota")
	frontendFixture(t, "usage-discount-self.get.json", selfBody)
}

// The body limit applies to the whole body: a complete first object followed
// by trailing data past the limit must not be accepted.
func TestUsageDiscountBodyLimitRejectsTrailingDataPastLimit(t *testing.T) {
	db := setupUsageDiscountControllerTest(t)
	engine := contractEngine(1, common.RoleRootUser)
	engine.PATCH("/api/option/usage_discount", UpdateUsageDiscountConfig)
	patch, err := common.Marshal(map[string]any{"expected_version": 0, "config": model.DefaultUsageDiscountConfig()})
	require.NoError(t, err)

	padded := bytes.Join([][]byte{patch, bytes.Repeat([]byte(" "), usageDiscountBodyLimit), []byte("{}")}, nil)
	tooLarge := contractRequest(t, engine, http.MethodPatch, "/api/option/usage_discount", padded)
	assert.Equal(t, http.StatusRequestEntityTooLarge, tooLarge.Code)
	assert.Equal(t, "usage_discount_invalid_config", decodeContractBody(t, tooLarge)["code"])

	trailing := contractRequest(t, engine, http.MethodPatch, "/api/option/usage_discount", append(patch, "{}"...))
	assert.Equal(t, http.StatusBadRequest, trailing.Code)

	var versions int64
	require.NoError(t, db.Model(&model.UsageDiscountConfigVersion{}).Count(&versions).Error)
	assert.Zero(t, versions)
}
