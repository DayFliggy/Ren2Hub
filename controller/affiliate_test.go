package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
)

func TestAffiliateHTTPTransferAndRecordOwnership(t *testing.T) {
	db := useGmpayControllerDB(t)
	policy := operation_setting.GetAffiliateSetting()
	old := *policy
	*policy = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000}
	payment := operation_setting.GetPaymentSetting()
	oldPayment := *payment
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() { *policy = old; *payment = oldPayment })
	user := model.User{Username: "referral-http", AffCode: "HTTP-CODE", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffQuota: 123, AffHistoryQuota: 123}
	other := model.User{Username: "referral-other", AffCode: "HTTP-OTHER", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&model.AffiliateRecord{UserID: other.Id, Kind: "accrue", SourceKey: "other-record", RewardQuota: 999}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id); c.Set("role", common.RoleCommonUser) })
	router.GET("/affiliate", GetAffiliateInfo)
	router.GET("/records", GetUserAffiliateRecords)
	router.POST("/transfer", TransferAffiliateQuota)
	invoke := func(method, path, body string) map[string]json.RawMessage {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	result := invoke(http.MethodGet, "/records?user_id=2", "")
	var page struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(result["data"], &page))
	assert.Zero(t, page.Total, "a user-supplied owner filter cannot expose another account")
	for _, body := range []string{`{"operation_id":"http-transfer-one","quota":1}`, `{"operation_id":"http-transfer-one","amount":1}`, `{"operation_id":"http-transfer-one"} {}`} {
		result = invoke(http.MethodPost, "/transfer", body)
		assert.JSONEq(t, "false", string(result["success"]))
	}
	result = invoke(http.MethodPost, "/transfer", `{"operation_id":"http-transfer-one"}`)
	var transfer struct {
		Transferred int  `json:"transferred_quota"`
		Replayed    bool `json:"replayed"`
	}
	require.NoError(t, json.Unmarshal(result["data"], &transfer))
	assert.Equal(t, 123, transfer.Transferred)
	assert.False(t, transfer.Replayed)
	result = invoke(http.MethodPost, "/transfer", `{"operation_id":"http-transfer-one"}`)
	require.NoError(t, json.Unmarshal(result["data"], &transfer))
	assert.True(t, transfer.Replayed)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 123, user.Quota)
	assert.Zero(t, user.AffQuota)
	result = invoke(http.MethodGet, "/affiliate", "")
	var info model.AffiliateInfo
	require.NoError(t, json.Unmarshal(result["data"], &info))
	assert.Equal(t, "HTTP-CODE", info.Code)
	assert.Equal(t, 123, info.RewardTotal)

	conflictingOperationID := "http-conflict-one"
	require.NoError(t, db.Create(&model.AffiliateRecord{
		UserID: user.Id, Kind: "accrue", SourceType: "admin_add", SourceID: "other",
		SourceKey: fmt.Sprintf("transfer:%d:%s", user.Id, conflictingOperationID), RewardQuota: 1,
	}).Error)
	request := httptest.NewRequest(http.MethodPost, "/transfer", bytes.NewBufferString(fmt.Sprintf(`{"operation_id":%q}`, conflictingOperationID)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
}

func TestAffiliateHTTPBackfillsLegacyRootInvitationCode(t *testing.T) {
	db := useGmpayControllerDB(t)
	root := model.User{Username: "legacy-root", Role: common.RoleRootUser, Quota: 1000, AffQuota: 123, AffHistoryQuota: 456}
	require.NoError(t, db.Create(&root).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", root.Id); c.Set("role", common.RoleRootUser) })
	router.GET("/api/user/affiliate", GetAffiliateInfo)
	var code string
	for attempt := range 2 {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/affiliate", nil))
		var result struct {
			Success bool                `json:"success"`
			Data    model.AffiliateInfo `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		assert.Regexp(t, `^[A-Z0-9_-]{12}$`, result.Data.Code)
		if attempt == 0 {
			code = result.Data.Code
		}
		assert.Equal(t, code, result.Data.Code)
		assert.Equal(t, 123, result.Data.AvailableReward)
		assert.Equal(t, 456, result.Data.TotalReward)
	}
	require.NoError(t, db.First(&root, root.Id).Error)
	assert.Equal(t, code, root.AffCode)
	assert.Equal(t, 1000, root.Quota)
	assert.Zero(t, root.InviterId)
}

func TestTopUpHTTPDetailAlwaysChecksOwner(t *testing.T) {
	db := useGmpayControllerDB(t)
	require.NoError(t, db.Create(&model.TopUp{UserId: 2, TradeNo: "another-wallet", Currency: "USD", PayAmount: "20"}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleRootUser) })
	router.GET("/detail/:trade_no", GetTopUpDetail)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/detail/another-wallet", nil))
	var result struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Empty(t, result.Data)
}

func TestStripePaymentMinorCurrency(t *testing.T) {
	for _, tc := range []struct {
		minor              int64
		currency, expected string
	}{{123, "usd", "1.23"}, {123, "jpy", "123"}, {1230, "kwd", "1.23"}, {12300, "ugx", "123"}} {
		assert.Equal(t, tc.expected, stripePaymentAmount(tc.minor, tc.currency))
	}
}

func TestStripeWebhookRetriesFailedAtomicCredit(t *testing.T) {
	db := useGmpayControllerDB(t)
	oldAPI, oldWebhook, oldPrice := setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId
	oldPayment := *operation_setting.GetPaymentSetting()
	setting.StripeApiSecret = "sk_test_local"
	setting.StripeWebhookSecret = "whsec_local_test"
	setting.StripePriceId = "price_local"
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = oldAPI, oldWebhook, oldPrice
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	user := model.User{Username: "stripe-retry", AffCode: "STRIPE-RETRY"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "stripe-retry-order", PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending, ExpectedQuota: 1000, Amount: 1, Money: 1, Currency: "USD", PayAmount: "1"}
	require.NoError(t, order.Insert())
	payload := []byte(`{"id":"evt_local","type":"checkout.session.completed","data":{"object":{"id":"cs_local","object":"checkout.session","mode":"payment","status":"complete","payment_status":"paid","customer":"cus_local","client_reference_id":"stripe-retry-order","amount_subtotal":100,"amount_total":99,"currency":"usd"}}}`)
	signature := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: setting.StripeWebhookSecret})
	router := gin.New()
	router.POST("/stripe", StripeWebhook)
	invoke := func() int {
		request := httptest.NewRequest(http.MethodPost, "/stripe", bytes.NewReader(payload))
		request.Header.Set("Stripe-Signature", signature.Header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail_stripe_wallet", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("test wallet unavailable"))
		}
	}))
	assert.Equal(t, http.StatusInternalServerError, invoke())
	require.NoError(t, db.Callback().Update().Remove("fail_stripe_wallet"))
	assert.Equal(t, http.StatusOK, invoke())
	assert.Equal(t, http.StatusOK, invoke())
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 1000, user.Quota)
	saved := model.GetTopUpByTradeNo(order.TradeNo)
	assert.Equal(t, "USD", saved.Currency)
	assert.Equal(t, "1", saved.PayAmount, "the frozen pre-discount amount is never overwritten by the callback")
	assert.Equal(t, 1000, saved.CreditedQuota)
}

func TestAffiliateHTTPInviteesNeverExposePrivateFieldsOrOtherOwner(t *testing.T) {
	db := useGmpayControllerDB(t)
	for _, user := range []model.User{
		{AffCode: "PRIVATE1", Username: "private-name", Email: "secret@example.test", Password: "secret-hash", AccessToken: nil, InviterId: 71, InvitedAt: 100},
		{AffCode: "FOREIGN1", Username: "foreign-name", InviterId: 72, InvitedAt: 200},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 71) })
	router.GET("/invitees", GetAffiliateInvitees)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/invitees?user_id=72&page_size=999", nil))
	var result struct {
		Success bool
		Data    struct {
			Total, PageSize int
			Items           []map[string]any
		}
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success)
	require.Len(t, result.Data.Items, 1)
	assert.Equal(t, 1, result.Data.Total)
	assert.Equal(t, "p***e", result.Data.Items[0]["masked_name"])
	assert.Len(t, result.Data.Items[0], 3)
	assert.NotContains(t, response.Body.String(), "secret")
	assert.NotContains(t, response.Body.String(), "private-name")
	assert.NotContains(t, response.Body.String(), "foreign-name")
}
