package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingAmountTokenResponseKeepsExactQuotaAndRouteType(t *testing.T) {
	token := &model.Token{Id: 1, RemainQuota: 9007199254740993, ReservedQuota: 100, UsedQuota: 200}
	response := tokenResponse{Token: token, Type: "manual"}
	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(encoded, &body))
	assert.Equal(t, "9007199254740993", body["available_quota"])
	assert.Equal(t, "100", body["reserved_quota"])
	assert.Equal(t, "manual", body["type"])
	token.UnlimitedQuota = true
	encoded, err = common.Marshal(response)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encoded, &body))
	assert.Nil(t, body["available_quota"])
	assert.Equal(t, "manual", body["type"])
}

func TestBillingAmountAdminAdjustmentRetriesAndConflicts(t *testing.T) {
	oldDB, oldLogDB, oldRedis, oldType := model.DB, model.LOG_DB, common.RedisEnabled, common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.BillingEvent{}, &model.Log{}))
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis
		common.SetMainDatabaseType(oldType)
	})
	user := model.User{Id: 1, Username: "adjusted", AffCode: "adjusted", Role: common.RoleCommonUser}
	require.NoError(t, db.Create(&user).Error)
	call := func(body string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(result)
		ctx.Set("id", 99)
		ctx.Set("role", common.RoleRootUser)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/next/admin/users/quota", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		NextAdminUserQuota(ctx)
		return result
	}
	payload := `{"id":1,"delta":"9007199254740993","operation_id":"exact-grant","expected_version":0,"reason":"approved grant"}`
	first := call(payload)
	assert.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), `"available_quota":"9007199254740993"`)
	replay := call(payload)
	assert.Equal(t, http.StatusOK, replay.Code)
	require.Contains(t, replay.Body.String(), `"available_quota":"9007199254740993"`)
	conflict := call(strings.Replace(payload, "exact-grant", "stale-grant", 1))
	assert.Equal(t, http.StatusConflict, conflict.Code)
	changed := call(strings.Replace(payload, "9007199254740993", "100", 1))
	assert.Equal(t, http.StatusConflict, changed.Code)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.Quota(9007199254740993), user.Quota)
	var count int64
	require.NoError(t, db.Model(&model.BillingEvent{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestBillingAmountRedemptionDecimalRemainsExact(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Redemption{}))
	settings := operation_setting.GetPaymentSetting()
	previous := *settings
	settings.ComplianceConfirmed = true
	settings.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() { *settings = previous })
	response := performNextAdminJSON(t, http.MethodPost, "/api/next/admin/redemptions", `{"type":"quota","count":1,"amount":"18014398509.481986","expired_time":-1}`, NextCreateAdminRedemptions)
	assert.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"quota":"9007199254740993"`)
	require.Contains(t, response.Body.String(), `"amount":"18014398509.481986"`)
	var redemption model.Redemption
	require.NoError(t, db.First(&redemption).Error)
	assert.Equal(t, common.Quota(9007199254740993), redemption.Quota)
	rejected := performNextAdminJSON(t, http.MethodPost, "/api/next/admin/redemptions", `{"type":"quota","count":1,"amount":18014398509.481986,"expired_time":-1}`, NextCreateAdminRedemptions)
	require.Contains(t, rejected.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, db.Model(&model.Redemption{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestBillingAmountNotificationThresholdRoundTrip(t *testing.T) {
	var request UpdateUserSettingRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"quota_warning_threshold":"9007199254740993"}`, &request))
	assert.Equal(t, common.Quota(9007199254740993), request.QuotaWarningThreshold)
	require.Error(t, common.UnmarshalJsonStr(`{"quota_warning_threshold":9007199254740993}`, &request))
	user := model.User{}
	user.SetSetting(dto.UserSetting{QuotaWarningThreshold: int64(request.QuotaWarningThreshold)})
	require.Contains(t, user.Setting, `"quota_warning_threshold":"9007199254740993"`)
	restored := user.GetSetting()
	assert.Equal(t, int64(9007199254740993), restored.QuotaWarningThreshold)
}
