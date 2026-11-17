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
// the balances after it, and audits a deduction once: a replay is not audited
// again.
func TestAdminAffiliateDeductionHTTPContractAndAudit(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.AuditLog{}))
	admin := model.User{Username: "deduct-http-admin", AffCode: "DEDUCT-HTTP-ADMIN", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	target := model.User{Username: "deduct-http-target", AffCode: "DEDUCT-HTTP-TARGET", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffQuota: 1000, AffHistoryQuota: 1000, AffFrozenQuota: 70}
	peer := model.User{Username: "deduct-http-peer", AffCode: "DEDUCT-HTTP-PEER", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffQuota: 1000}
	for _, user := range []*model.User{&admin, &target, &peer} {
		require.NoError(t, db.Create(user).Error)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", admin.Id); c.Set("role", common.RoleAdminUser) })
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
	audits := func() int64 {
		var count int64
		require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("action = ?", "user.affiliate_deduct").Count(&count).Error)
		return count
	}

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
	assert.EqualValues(t, 1, audits())

	status, result = deduct(target.Id, `{"quota":400,"operation_id":"deduct-http-one"}`)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(result["data"], &data))
	assert.JSONEq(t, "true", string(data["replayed"]))
	assert.JSONEq(t, "600", string(data["available_quota"]))
	assert.EqualValues(t, 1, audits(), "a replay is not audited again")

	status, result = deduct(target.Id, `{"quota":401,"operation_id":"deduct-http-one"}`)
	assert.Equal(t, http.StatusConflict, status)
	assert.JSONEq(t, "false", string(result["success"]))

	var after model.User
	require.NoError(t, db.First(&after, target.Id).Error)
	assert.Equal(t, 600, after.AffQuota)
	assert.Equal(t, 1000, after.AffHistoryQuota)
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
