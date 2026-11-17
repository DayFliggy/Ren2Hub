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
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func paidReversalTopUp(t *testing.T, db *gorm.DB, userID int, trade string) {
	t.Helper()
	order := &model.TopUp{UserId: userID, TradeNo: trade, PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, ExpectedQuota: 10000, Money: 2, Currency: "CNY", PayAmount: "2.00", CreateTime: common.GetTimestamp()}
	require.NoError(t, order.Insert())
	_, err := model.RechargeEpay(trade, "", "", "2.00")
	require.NoError(t, err)
}

// The administrator order detail shows the order's referral reward and whether
// it was reversed; the owner's detail never carries it. The reversal endpoint
// decodes one strict JSON object, returns the reversal and its effect, and
// audits a reversal once with its operator and every business field; a replay
// or a failure changes no audit.
func TestTopUpAffiliateReversalHTTPContractAndAudit(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.AuditLog{}))
	policy, payment := operation_setting.GetAffiliateSetting(), operation_setting.GetPaymentSetting()
	oldPolicy, oldPayment := *policy, *payment
	t.Cleanup(func() { *policy, *payment = oldPolicy, oldPayment })
	*policy = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000}
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	admin := model.User{Username: "reverse-http-admin", AffCode: "REVERSE-HTTP-ADMIN", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	inviter := model.User{Username: "reverse-http-inviter", AffCode: "REVERSE-HTTP-INVITER", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	peer := model.User{Username: "reverse-http-peer", AffCode: "REVERSE-HTTP-PEER", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	for _, user := range []*model.User{&admin, &inviter, &peer} {
		require.NoError(t, db.Create(user).Error)
	}
	invitee := model.User{Username: "reverse-http-invitee", AffCode: "REVERSE-HTTP-INVITEE", InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Status: common.UserStatusEnabled}
	peerInvitee := model.User{Username: "reverse-http-peer-invitee", AffCode: "REVERSE-HTTP-PEER-INVITEE", InviterId: peer.Id, InvitedAt: common.GetTimestamp(), Status: common.UserStatusEnabled}
	loner := model.User{Username: "reverse-http-loner", AffCode: "REVERSE-HTTP-LONER", Status: common.UserStatusEnabled}
	for _, user := range []*model.User{&invitee, &peerInvitee, &loner} {
		require.NoError(t, db.Create(user).Error)
	}
	// The reversed order is paid last, so its id differs from the user ids.
	paidReversalTopUp(t, db, loner.Id, "reverse-http-loner-order")
	paidReversalTopUp(t, db, peerInvitee.Id, "reverse-http-peer-order")
	paidReversalTopUp(t, db, invitee.Id, "reverse-http-order")
	// 700 of the 2000 reward is gone, so the reversal takes back 1300 and owes
	// 700: reward, revoked quota and debt all differ.
	_, _, err := model.DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 700, "reverse-http-deduct")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		id := admin.Id
		if c.Query("as") == "owner" {
			id = invitee.Id
		}
		c.Set("id", id)
		c.Set("username", admin.Username)
		c.Set("role", common.RoleAdminUser)
	})
	router.GET("/detail/:trade_no", GetTopUpDetail)
	router.GET("/admin/detail/:trade_no", GetAdminTopUpDetail)
	router.POST("/admin/affiliate-reversal", ReverseTopUpAffiliateReward)
	call := func(method, path, body string) map[string]json.RawMessage {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, path)
		var result map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
		return result
	}
	data := func(result map[string]json.RawMessage) map[string]json.RawMessage {
		require.JSONEq(t, "true", string(result["success"]), string(result["message"]))
		var value map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(result["data"], &value))
		return value
	}
	audits := func() int64 { return int64(len(operationAudits(t, "topup.affiliate_reverse"))) }

	detail := data(call(http.MethodGet, "/admin/detail/reverse-http-order", ""))
	assert.JSONEq(t, `{"inviter_id":`+strconv.Itoa(inviter.Id)+`,"reward_quota":2000,"reversed":false}`, string(detail["affiliate"]))
	assert.JSONEq(t, "null", string(data(call(http.MethodGet, "/admin/detail/reverse-http-loner-order", ""))["affiliate"]))
	owner := data(call(http.MethodGet, "/detail/reverse-http-order?as=owner", ""))
	assert.NotContains(t, owner, "affiliate", "the owner's detail never carries the referral reward")

	for _, body := range []string{
		`{"trade_no":"reverse-http-order","note":"x"}`,
		`{"trade_no":"reverse-http-order"} {}`,
		`{"trade_no":7}`,
	} {
		assert.JSONEq(t, "false", string(call(http.MethodPost, "/admin/affiliate-reversal", body)["success"]), body)
	}
	for body, message := range map[string]string{
		`{"trade_no":"reverse-http-peer-order"}`:  "No permission to update users of same or higher permission level",
		`{"trade_no":"reverse-http-missing"}`:     "Top-up order does not exist",
		`{"trade_no":"reverse-http-loner-order"}`: "this order has no referral reward to reverse",
	} {
		result := call(http.MethodPost, "/admin/affiliate-reversal", body)
		assert.JSONEq(t, "false", string(result["success"]), body)
		assert.JSONEq(t, `"`+message+`"`, string(result["message"]), body)
	}
	require.Zero(t, audits())

	reversal := data(call(http.MethodPost, "/admin/affiliate-reversal", `{"trade_no":"reverse-http-order"}`))
	assert.ElementsMatch(t, []string{"record", "replayed", "reversed_quota", "revoked_quota", "debt_added_quota"}, mapKeys(reversal))
	assert.JSONEq(t, "false", string(reversal["replayed"]))
	assert.JSONEq(t, "2000", string(reversal["reversed_quota"]))
	assert.JSONEq(t, "1300", string(reversal["revoked_quota"]))
	assert.JSONEq(t, "700", string(reversal["debt_added_quota"]))
	var record struct {
		Kind       string `json:"kind"`
		SourceType string `json:"source_type"`
		SourceID   string `json:"source_id"`
		InviteeID  int    `json:"invitee_id"`
		Credited   int    `json:"credited_quota"`
	}
	require.NoError(t, json.Unmarshal(reversal["record"], &record))
	assert.Equal(t, "reversal", record.Kind)
	assert.Equal(t, "topup_reversal", record.SourceType)
	assert.Equal(t, invitee.Id, record.InviteeID)
	assert.Equal(t, 1300, record.Credited)
	topUpID, err := strconv.Atoi(record.SourceID)
	require.NoError(t, err)
	requireDistinctValues(t, admin.Id, inviter.Id, invitee.Id, topUpID, 2000, 1300, 700)
	recorded := operationAudits(t, "topup.affiliate_reverse")
	require.Len(t, recorded, 1)
	assertOperationAudit(t, recorded[0], admin, "topup.affiliate_reverse", "Reversed the referral reward of a top-up order", map[string]any{
		"trade_no": "reverse-http-order", "topup_id": record.SourceID, "inviter_id": inviter.Id, "invited_user_id": invitee.Id,
		"reward_quota": 2000, "revoked_quota": 1300, "debt_added_quota": 700,
	})

	replay := data(call(http.MethodPost, "/admin/affiliate-reversal", `{"trade_no":"reverse-http-order"}`))
	assert.JSONEq(t, "true", string(replay["replayed"]))
	assert.JSONEq(t, "1300", string(replay["revoked_quota"]))
	assert.JSONEq(t, "false", string(call(http.MethodPost, "/admin/affiliate-reversal", `{"trade_no":"reverse-http-peer-order"}`)["success"]))
	assert.Equal(t, recorded, operationAudits(t, "topup.affiliate_reverse"), "a replay or a failure leaves the audit as it was")

	detail = data(call(http.MethodGet, "/admin/detail/reverse-http-order", ""))
	assert.JSONEq(t, `{"inviter_id":`+strconv.Itoa(inviter.Id)+`,"reward_quota":2000,"reversed":true}`, string(detail["affiliate"]))
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Equal(t, 700, inviter.AffOffsetDebtQuota)
	assert.Equal(t, 2000, inviter.AffHistoryQuota)
}
