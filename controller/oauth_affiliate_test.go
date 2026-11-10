package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOAuthAffiliateErrorsArePublicAndRegistrationIsAtomic(t *testing.T) {
	for _, flow := range []string{"oauth", "wechat", "password"} {
		for _, test := range []struct {
			name, code, affiliate string
			enabled, codeEnabled  bool
			err                   error
		}{
			{"disabled_program", "AFFILIATE_DISABLED", "STALE-CODE", false, true, model.ErrAffiliateDisabled},
			{"unknown_code", "AFFILIATE_CODE_INVALID", "GONE-CODE", true, true, model.ErrAffiliateCodeInvalid},
			{"disabled_code", "AFFILIATE_CODE_INVALID", "STALE-CODE", true, false, model.ErrAffiliateCodeInvalid},
			{"storage_failure", "AUTH_INTERNAL_ERROR", "", true, true, errors.New("private INSERT failure; user state must not escape")},
		} {
			if flow == "password" && test.name == "storage_failure" {
				continue
			}
			t.Run(flow+"/"+test.name, func(t *testing.T) {
				inviter, _ := setupSecurityEnrollmentTest(t)
				oldRegister, oldPassword, oldQuota := common.RegisterEnabled, common.PasswordRegisterEnabled, common.QuotaForNewUser
				oldAffiliate := *operation_setting.GetAffiliateSetting()
				common.RegisterEnabled, common.PasswordRegisterEnabled, common.QuotaForNewUser = true, true, 0
				oldEmail := common.EmailVerificationEnabled
				common.EmailVerificationEnabled = false
				t.Cleanup(func() { common.EmailVerificationEnabled = oldEmail })
				*operation_setting.GetAffiliateSetting() = operation_setting.AffiliateSetting{Enabled: test.enabled, RateBps: 2000}
				t.Cleanup(func() {
					common.RegisterEnabled, common.PasswordRegisterEnabled, common.QuotaForNewUser = oldRegister, oldPassword, oldQuota
					*operation_setting.GetAffiliateSetting() = oldAffiliate
				})
				require.NoError(t, model.DB.Model(inviter).Updates(map[string]any{"aff_code": "STALE-CODE", "aff_code_enabled": test.codeEnabled}).Error)
				if test.name == "storage_failure" {
					require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("oauth_affiliate_storage_error", func(tx *gorm.DB) {
						if tx.Statement.Table == "users" {
							tx.AddError(test.err)
						}
					}))
				}
				response := performAffiliateOAuthRegistration(t, flow, test.affiliate)
				var body struct {
					Success    bool   `json:"success"`
					Code       string `json:"code"`
					Message    string `json:"message"`
					MessageKey string `json:"message_key"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				assert.False(t, body.Success)
				assert.Equal(t, test.code, body.Code)
				if test.name == "storage_failure" {
					assert.Equal(t, http.StatusInternalServerError, response.Code)
					assert.Equal(t, "Internal Server Error", body.Message)
					assert.Empty(t, body.MessageKey)
				} else {
					assert.Equal(t, http.StatusOK, response.Code)
					assert.Equal(t, test.err.Error(), body.Message)
					assert.Equal(t, test.err.Error(), body.MessageKey)
				}
				assert.NotContains(t, response.Body.String(), "private")
				assert.NotContains(t, response.Body.String(), "INSERT")
				var count int64
				require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
				assert.EqualValues(t, 1, count, "rejected registration must not persist a new account")
				require.NoError(t, model.DB.First(inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffCount, "rejected registration must not change referral counts")
			})
		}
	}
}

func performAffiliateOAuthRegistration(t *testing.T, flow, affiliate string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	if flow == "password" {
		router.POST("/api/user/register", Register)
		body, err := common.Marshal(map[string]string{"username": "referral_new_user", "password": "ReferralTestPassword123!", "aff_code": affiliate})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	path := "/api/oauth/wechat?code=test&aff=" + url.QueryEscape(affiliate)
	if flow == "wechat" {
		oldEnabled, oldAddress, oldToken := common.WeChatAuthEnabled, common.WeChatServerAddress, common.WeChatServerToken
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, err := w.Write([]byte(`{"success":true,"data":"affiliate-wechat-new-user"}`))
			require.NoError(t, err)
		}))
		t.Cleanup(server.Close)
		common.WeChatAuthEnabled, common.WeChatServerAddress, common.WeChatServerToken = true, server.URL, "affiliate-test-local"
		t.Cleanup(func() {
			common.WeChatAuthEnabled, common.WeChatServerAddress, common.WeChatServerToken = oldEnabled, oldAddress, oldToken
		})
		router.GET("/api/oauth/wechat", WeChatAuth)
	} else {
		oauth.Register("affiliate-test", &affiliateTestOAuthProvider{})
		t.Cleanup(func() { oauth.Unregister("affiliate-test") })
		router.POST("/api/oauth/state", GenerateOAuthCode)
		router.GET("/api/oauth/:provider", HandleOAuth)
		body, err := common.Marshal(map[string]string{"provider": "affiliate-test", "intent": "login", "aff": affiliate})
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		stateResponse := httptest.NewRecorder()
		router.ServeHTTP(stateResponse, request)
		var state struct {
			Success bool `json:"success"`
			Data    struct {
				FlowToken string `json:"flow_token"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(stateResponse.Body.Bytes(), &state))
		require.True(t, state.Success, stateResponse.Body.String())
		path = "/api/oauth/affiliate-test?code=test&state=" + url.QueryEscape(state.Data.FlowToken)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestOAuthAffiliateErrorMappingDiscardsWrappedPrivateDetails(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{model.ErrAffiliateDisabled, "AFFILIATE_DISABLED"},
		{model.ErrAffiliateCodeInvalid, "AFFILIATE_CODE_INVALID"},
	} {
		t.Run(test.code, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/oauth/provider", nil)
			writeSecurityOperationError(ctx, fmt.Errorf("private query details: %w", test.err))
			var body map[string]any
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, test.code, body["code"])
			assert.Equal(t, test.err.Error(), body["message_key"])
			assert.Equal(t, test.err.Error(), body["message"])
			assert.NotContains(t, response.Body.String(), "private")
		})
	}
}

// Upstream 48a6c87f9 adds `user.InviterId = inviterId` before InsertWithTx.
// Ren2Hub does not take that line: InsertWithTx initializes the referral from
// RegistrationAffCode in the same transaction. This guards that path.
func TestOAuthRegistrationPersistsInviterOnce(t *testing.T) {
	for _, flow := range []string{"oauth", "wechat"} {
		t.Run(flow, func(t *testing.T) {
			inviter, _ := setupSecurityEnrollmentTest(t)
			oldRegister, oldQuota := common.RegisterEnabled, common.QuotaForNewUser
			oldAffiliate := *operation_setting.GetAffiliateSetting()
			common.RegisterEnabled, common.QuotaForNewUser = true, 0
			*operation_setting.GetAffiliateSetting() = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000}
			t.Cleanup(func() {
				common.RegisterEnabled, common.QuotaForNewUser = oldRegister, oldQuota
				*operation_setting.GetAffiliateSetting() = oldAffiliate
			})
			require.NoError(t, model.DB.Model(inviter).Updates(map[string]any{"aff_code": "LIVE-CODE", "aff_code_enabled": true}).Error)

			// Codes are normalized to upper case before lookup.
			response := performAffiliateOAuthRegistration(t, flow, "live-code")
			require.Contains(t, response.Body.String(), `"success":true`, response.Body.String())
			var invitee model.User
			require.NoError(t, model.DB.Where("id <> ?", inviter.Id).First(&invitee).Error)
			assert.Equal(t, inviter.Id, invitee.InviterId)
			assert.Positive(t, invitee.InvitedAt)
			require.NoError(t, model.DB.First(inviter, inviter.Id).Error)
			assert.Equal(t, 1, inviter.AffCount)

			// Signing in again with the same code finds the existing account and
			// neither changes the inviter nor counts the referral twice.
			response = performAffiliateOAuthRegistration(t, flow, "live-code")
			require.Contains(t, response.Body.String(), `"success":true`, response.Body.String())
			var count int64
			require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
			assert.EqualValues(t, 2, count)
			var again model.User
			require.NoError(t, model.DB.First(&again, invitee.Id).Error)
			assert.Equal(t, inviter.Id, again.InviterId)
			assert.Equal(t, invitee.InvitedAt, again.InvitedAt)
			require.NoError(t, model.DB.First(inviter, inviter.Id).Error)
			assert.Equal(t, 1, inviter.AffCount)
		})
	}
}

// affiliateTestOAuthProvider stores its account in oidc_id, so a second
// sign-in finds the user created by the first one.
type affiliateTestOAuthProvider struct{ authFlowTestOAuthProvider }

func (*affiliateTestOAuthProvider) IsUserIDTaken(id string) bool {
	return model.IsOidcIdAlreadyTaken(id)
}
func (*affiliateTestOAuthProvider) FillUserByProviderID(user *model.User, id string) error {
	user.OidcId = id
	return user.FillUserByOidcId()
}
func (*affiliateTestOAuthProvider) SetProviderUserID(user *model.User, id string) {
	user.OidcId = id
}
func (*affiliateTestOAuthProvider) ProviderUserIDColumn() string { return "oidc_id" }
