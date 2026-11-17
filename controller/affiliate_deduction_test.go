package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The administrator deduction endpoint decodes one strict JSON object, answers
// a reused operation with another amount with 409, returns the deduction and
// the balances after it, and audits a deduction once with its operator and
// every business field; a replay or a failure changes no audit.
func TestAdminAffiliateDeductionHTTPContractAndAudit(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.AuditLog{}))
	admin := model.User{Username: "deduct-http-admin", AffCode: "DEDUCT-HTTP-ADMIN", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	target := model.User{Username: "deduct-http-target", AffCode: "DEDUCT-HTTP-TARGET", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffQuota: 1000, AffHistoryQuota: 1000, AffFrozenQuota: 70}
	peer := model.User{Username: "deduct-http-peer", AffCode: "DEDUCT-HTTP-PEER", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffQuota: 1000}
	for _, user := range []*model.User{&admin, &target, &peer} {
		require.NoError(t, db.Create(user).Error)
	}
	// Unrelated rows move the new record's id away from the user ids, so an
	// audit field written from the wrong value cannot match by accident.
	for i := range 4 {
		require.NoError(t, db.Create(&model.AffiliateRecord{UserID: peer.Id, Kind: "accrue", SourceType: "topup", SourceID: strconv.Itoa(i), SourceKey: "deduct-http-filler:" + strconv.Itoa(i)}).Error)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", admin.Id)
		c.Set("username", admin.Username)
		c.Set("role", common.RoleAdminUser)
	})
	router.POST("/api/admin/affiliate/users/:id/deduct", DeductAffiliateQuota)
	deduct := func(userID int, body string) (int, map[string]json.RawMessage) {
		request := httptest.NewRequest(http.MethodPost, "/api/admin/affiliate/users/"+strconv.Itoa(userID)+"/deduct", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var result map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
		return response.Code, result
	}
	audits := func() int64 { return int64(len(operationAudits(t, "user.affiliate_deduct"))) }

	for _, body := range []string{
		`{"quota":100,"operation_id":"deduct-http-one","note":"x"}`,
		`{"quota":100,"operation_id":"deduct-http-one"} {}`,
		`{"quota":1.5,"operation_id":"deduct-http-one"}`,
		`{"quota":0,"operation_id":"deduct-http-one"}`,
	} {
		status, result := deduct(target.Id, body)
		assert.Equal(t, http.StatusOK, status, body)
		assert.JSONEq(t, "false", string(result["success"]), body)
	}
	status, result := deduct(peer.Id, `{"quota":100,"operation_id":"deduct-http-peer"}`)
	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, "false", string(result["success"]))
	assert.JSONEq(t, `"No permission to update users of same or higher permission level"`, string(result["message"]))
	status, result = deduct(target.Id+1000, `{"quota":100,"operation_id":"deduct-http-missing"}`)
	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `"User does not exist"`, string(result["message"]))
	require.Zero(t, audits())

	status, result = deduct(target.Id, `{"quota":400,"operation_id":"deduct-http-one"}`)
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, "true", string(result["success"]), string(result["message"]))
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result["data"], &data))
	assert.ElementsMatch(t, []string{"record", "replayed", "deducted_quota", "available_quota", "frozen_quota"}, mapKeys(data))
	assert.JSONEq(t, "false", string(data["replayed"]))
	assert.JSONEq(t, "400", string(data["deducted_quota"]))
	assert.JSONEq(t, "600", string(data["available_quota"]))
	assert.JSONEq(t, "70", string(data["frozen_quota"]))
	var record struct {
		ID         int    `json:"id"`
		Kind       string `json:"kind"`
		SourceType string `json:"source_type"`
		SourceID   string `json:"source_id"`
		Reward     int    `json:"reward_quota"`
	}
	require.NoError(t, json.Unmarshal(data["record"], &record))
	assert.Equal(t, "deduction", record.Kind)
	assert.Equal(t, "admin_deduction", record.SourceType)
	assert.Equal(t, "deduct-http-one", record.SourceID)
	assert.Equal(t, 400, record.Reward)
	requireDistinctValues(t, admin.Id, target.Id, record.ID, 400, 600)
	recorded := operationAudits(t, "user.affiliate_deduct")
	require.Len(t, recorded, 1)
	assertOperationAudit(t, recorded[0], admin, "user.affiliate_deduct", "Deducted user referral quota", map[string]any{
		"target_user_id": target.Id, "quota": 400, "operation_id": "deduct-http-one", "record_id": record.ID, "available_after": 600,
	})

	status, result = deduct(target.Id, `{"quota":400,"operation_id":"deduct-http-one"}`)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(result["data"], &data))
	assert.JSONEq(t, "true", string(data["replayed"]))
	assert.JSONEq(t, "600", string(data["available_quota"]))

	status, result = deduct(target.Id, `{"quota":401,"operation_id":"deduct-http-one"}`)
	assert.Equal(t, http.StatusConflict, status)
	assert.JSONEq(t, "false", string(result["success"]))
	status, result = deduct(peer.Id, `{"quota":100,"operation_id":"deduct-http-peer"}`)
	assert.JSONEq(t, "false", string(result["success"]))
	assert.Equal(t, recorded, operationAudits(t, "user.affiliate_deduct"), "a replay or a failure leaves the audit as it was")

	var after model.User
	require.NoError(t, db.First(&after, target.Id).Error)
	assert.Equal(t, 600, after.AffQuota)
	assert.Equal(t, 1000, after.AffHistoryQuota)
}

// operationAudits returns the audit rows of one action in id order.
func operationAudits(t *testing.T, action string) []model.AuditLog {
	t.Helper()
	var rows []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ?", action).Order("id ASC").Find(&rows).Error)
	return rows
}

// assertOperationAudit checks the operator, action, content and every business
// field of one administrator audit row.
func assertOperationAudit(t *testing.T, audit model.AuditLog, operator model.User, action, content string, params map[string]any) {
	t.Helper()
	assert.Equal(t, operator.Id, audit.UserId)
	assert.Equal(t, operator.Username, audit.Username)
	assert.Equal(t, operator.Role, audit.ActorRole)
	assert.Equal(t, action, audit.Action)
	assert.Equal(t, content, audit.Content)
	assert.True(t, audit.Success)
	require.NotNil(t, audit.Other.Op)
	assert.Equal(t, action, audit.Other.Op.Action)
	require.NotNil(t, audit.Other.AdminInfo)
	assert.Equal(t, operator.Id, audit.Other.AdminInfo.AdminID)
	assert.Equal(t, operator.Username, audit.Other.AdminInfo.AdminUsername)
	assert.Equal(t, operator.Role, audit.Other.AdminInfo.AdminRole)
	want, err := json.Marshal(params)
	require.NoError(t, err)
	got, err := json.Marshal(audit.Other.Op.Params)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

// requireDistinctValues guards test data whose audit fields are compared: a
// field written from another field's value must not match by coincidence.
func requireDistinctValues(t *testing.T, values ...int) {
	t.Helper()
	seen := map[int]bool{}
	for _, value := range values {
		require.False(t, seen[value], "test values must be distinct: %v", values)
		seen[value] = true
	}
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
