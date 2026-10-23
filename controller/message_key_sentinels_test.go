package controller

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The local sentinels below reach the console; each response must carry the
// message_key the zh-CN server-messages translate.

type sentinelResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	MessageKey string `json:"message_key"`
}

func TestTokenRoutingErrorsCarryStableMessageKeys(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		key    string
	}{
		{fmt.Errorf("private context: %w", model.ErrTokenRouteInvalid), http.StatusBadRequest, model.ErrTokenRouteInvalid.Error()},
		{model.ErrTokenRouteForbidden, http.StatusForbidden, model.ErrTokenRouteForbidden.Error()},
		{model.ErrTokenRouteConflict, http.StatusConflict, model.ErrTokenRouteConflict.Error()},
		{gorm.ErrRecordNotFound, http.StatusNotFound, "Token not found"},
		{errors.New("storage failed"), http.StatusInternalServerError, "Unable to read or save token routing"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			tokenRoutingError(ctx, tc.err)
			body := decodeSentinelResponse(t, recorder)
			assert.Equal(t, tc.status, recorder.Code)
			assert.Equal(t, tc.key, body.MessageKey)
			assert.Equal(t, tc.key, body.Message)
		})
	}
}

func decodeSentinelResponse(t *testing.T, recorder *httptest.ResponseRecorder) sentinelResponse {
	t.Helper()
	var body sentinelResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.False(t, body.Success)
	return body
}

func TestRegisterWithDisabledReferralReturnsMessageKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useGmpayControllerDB(t)
	setting := operation_setting.GetAffiliateSetting()
	oldEnabled := setting.Enabled
	oldRegister, oldPassword, oldEmail := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled
	t.Cleanup(func() {
		setting.Enabled = oldEnabled
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = oldRegister, oldPassword, oldEmail
	})
	setting.Enabled = false
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = true, true, false

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewBufferString(`{"username":"invitee","password":"invitee-pass","aff_code":"INVITER"}`))
	Register(ctx)

	body := decodeSentinelResponse(t, recorder)
	assert.Equal(t, "referral program is disabled; remove the invitation code to register", body.MessageKey)
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "invitee").Count(&count).Error)
	assert.Zero(t, count)
}

func TestAdminTicketStatusRepliedReturnsMessageKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTicketTestDatabase(t)
	ticket := &model.Ticket{UserID: 5, Title: "t", Category: "api", Priority: "normal"}
	_, err := model.CreateTicket(ticket, "c", 5, nil)
	require.NoError(t, err)

	// replied is set by support replies only, never through the status endpoint.
	ctx, recorder := adminTicketContext(http.MethodPatch, ticket.ID, bytes.NewReader([]byte(`{"status":"replied"}`)), "application/json")
	UpdateAdminTicketStatus(ctx)

	body := decodeSentinelResponse(t, recorder)
	assert.Equal(t, "Invalid ticket status", body.MessageKey)
	stored, err := model.GetTicket(ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, model.TicketStatusOpen, stored.Status)
}

func TestManualCompleteWithoutGmpaySnapshotReturnsMessageKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := useGmpayControllerDB(t)
	user := model.User{Username: "gmpay-no-snapshot", AffCode: "gmpay-no-snapshot"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "gmpay-no-snapshot", Amount: 10, PaymentProvider: model.PaymentProviderGmpay, Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&order).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/topup/complete", bytes.NewBufferString(fmt.Sprintf(`{"trade_no":%q}`, order.TradeNo)))
	AdminCompleteTopUp(ctx)

	body := decodeSentinelResponse(t, recorder)
	assert.Equal(t, "gmpay order snapshot is missing", body.MessageKey)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
}

// Reusing an operation_id for a different adjustment is a 409 with a
// translatable message, and the balance keeps the first adjustment only.
func TestManageUserQuotaOperationConflictReturnsMessageKey(t *testing.T) {
	db := setupManageUserTestDB(t)
	createQuotaTestOperator(t, db, common.RoleRootUser)
	user := model.User{Username: "quota-owner", Role: common.RoleCommonUser, Quota: 1000, AffCode: "quota-conflict-aff"}
	require.NoError(t, db.Create(&user).Error)
	request := func(value int) *httptest.ResponseRecorder {
		return performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"add_quota","operation_id":"quota-conflict-operation","mode":"add","value":%d}`, user.Id, value))
	}

	require.Equal(t, http.StatusOK, request(500).Code)
	conflict := request(700)

	assert.Equal(t, http.StatusConflict, conflict.Code)
	body := decodeSentinelResponse(t, conflict)
	assert.Equal(t, "operation_id was already used for a different request", body.MessageKey)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1500, user.Quota)
}
