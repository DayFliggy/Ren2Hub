package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saveFixedTopUpBonus(t *testing.T, rate string) {
	t.Helper()
	config := model.DefaultTopUpBonusConfig()
	config.CapRate, config.FixedRate = rate, rate
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveTopUpBonusConfig(data, 0, 1, time.Now())
	require.NoError(t, err)
}

// An administrator addition credits principal plus bonus once, reports the
// bonus and records it in the recipient's log; a replay repeats the answer.
func TestManageUserQuotaAdditionCreditsBonusOnce(t *testing.T) {
	db := setupManageUserTestDB(t)
	createQuotaTestOperator(t, db, common.RoleRootUser)
	user := model.User{Username: "bonus-recipient", Role: common.RoleCommonUser, Quota: 1000, AffCode: "BONUS-RECIPIENT"}
	require.NoError(t, db.Create(&user).Error)
	saveFixedTopUpBonus(t, "0.1")

	body := fmt.Sprintf(`{"id":%d,"action":"add_quota","operation_id":"bonus-addition","mode":"add","value":1000}`, user.Id)
	for attempt := range 2 {
		response := performManageUserRequest(t, body)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Before, After int
				Replayed      bool
				BonusQuota    int `json:"bonus_quota"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		assert.Equal(t, 1000, result.Data.Before)
		assert.Equal(t, 2100, result.Data.After)
		assert.Equal(t, 100, result.Data.BonusQuota)
		assert.Equal(t, attempt == 1, result.Data.Replayed)
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 2100, user.Quota)

	logs, _, err := model.GetUserLogs(user.Id, model.LogFilter{Type: model.LogTypeTopup}, 0, 20)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "Increased user quota by 1000, including a bonus of 100", logs[0].Content)
	var other model.AuditOther
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	require.NotNil(t, other.Op)
	params, err := common.Marshal(other.Op.Params)
	require.NoError(t, err)
	assert.Contains(t, string(params), `"quota":1000`, "quota stays the principal")
	assert.Contains(t, string(params), `"bonus_quota":100`)

	// A subtraction never decides a bonus.
	response := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","operation_id":"bonus-subtraction","mode":"subtract","value":100}`, user.Id))
	assert.Contains(t, response.Body.String(), `"bonus_quota":0`)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 2000, user.Quota)
}

func TestRedeemReportsBonus(t *testing.T) {
	db := setupTopUpBonusControllerTest(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	payment := operation_setting.GetPaymentSetting()
	oldPayment := *payment
	t.Cleanup(func() { *operation_setting.GetPaymentSetting() = oldPayment })
	payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	user := model.User{Username: "bonus-redeemer", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "BONUS-REDEEMER"}
	require.NoError(t, db.Create(&user).Error)
	code := model.Redemption{UserId: user.Id, Key: "bonus-redeem-key", Name: "bonus", Status: common.RedemptionCodeStatusEnabled, Quota: 1000, CreatedTime: common.GetTimestamp()}
	require.NoError(t, db.Create(&code).Error)
	saveFixedTopUpBonus(t, "0.05")

	engine := contractEngine(user.Id, common.RoleCommonUser)
	engine.POST("/api/user/topup", TopUp)
	body := decodeContractBody(t, contractRequest(t, engine, http.MethodPost, "/api/user/topup", []byte(`{"key":"bonus-redeem-key"}`)))
	require.Equal(t, true, body["success"], body)
	assert.Equal(t, 1050.0, body["data"], "data stays the credited total")
	assert.Equal(t, 50.0, body["bonus_quota"])
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1050, user.Quota)
}
