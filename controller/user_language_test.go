package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The token created at registration is named in the language the request
// states, English when it states none.
func TestRegisterNamesTheDefaultTokenInTheRequestLanguage(t *testing.T) {
	setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.BillingEvent{}))
	previous := []bool{common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken}
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken = true, true, false, true
	t.Cleanup(func() {
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled, constant.GenerateDefaultToken = previous[0], previous[1], previous[2], previous[3]
	})

	for _, tc := range []struct {
		username       string
		acceptLanguage string
		want           string
	}{
		{"newcomer-cn", "zh-CN,zh;q=0.9", "newcomer-cn的初始令牌"},
		{"newcomer-en", "", "newcomer-en's initial token"},
	} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"username":"`+tc.username+`","password":"newcomer-password"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		if tc.acceptLanguage != "" {
			c.Request.Header.Set("Accept-Language", tc.acceptLanguage)
		}
		Register(c)
		require.JSONEq(t, `{"success":true,"message":""}`, response.Body.String(), tc.username)

		var token model.Token
		require.NoError(t, model.DB.Joins("JOIN users ON users.id = tokens.user_id").Where("users.username = ?", tc.username).First(&token).Error)
		assert.Equal(t, tc.want, token.Name)
	}
}

// The account language selects the language of backend messages, emails and
// notifications. Only en, zh-CN and zh-TW are saved, aliases in their canonical
// form, and the cached user that TokenAuth reads follows at once.
func TestUpdateSelfSavesOnlySupportedLanguages(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	previousRDB := common.RDB
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = previousRDB, false
		require.NoError(t, client.Close())
	})
	cached, err := model.GetUserCache(user.Id)
	require.NoError(t, err)
	require.Empty(t, cached.GetSetting().Language)

	for _, tc := range []struct {
		body   string
		status int
		want   string
	}{
		{`{"language":"zhTW"}`, http.StatusOK, "zh-TW"},
		{`{"language":"en-US"}`, http.StatusOK, "en"},
		{`{"language":"zh_CN"}`, http.StatusOK, "zh-CN"},
		{`{"language":"fr"}`, http.StatusBadRequest, "zh-CN"},
		{`{"language":""}`, http.StatusBadRequest, "zh-CN"},
		{`{"language":7}`, http.StatusBadRequest, "zh-CN"},
	} {
		response := securityEnrollmentRequest(http.MethodPut, "/api/user/self", tc.body, "", identity, UpdateSelf)
		assert.Equal(t, tc.status, response.Code, tc.body)
		if tc.status == http.StatusBadRequest {
			assert.JSONEq(t, `{"success":false,"message":"Unsupported language","message_key":"Unsupported language"}`, response.Body.String(), tc.body)
		}
		stored, err := model.GetUserById(user.Id, true)
		require.NoError(t, err)
		assert.Equal(t, tc.want, stored.GetSetting().Language, tc.body)
		cached, err := model.GetUserCache(user.Id)
		require.NoError(t, err)
		assert.Equal(t, tc.want, cached.GetSetting().Language, tc.body)
	}
}

// A verification mail goes to an address that may have no account yet, so it
// follows the request language. A password reset mail follows the language
// saved on the account, then the request language.
func TestAccountEmailsFollowTheReaderLanguage(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	mailbox := newSecurityMailbox(t)
	previousSystemName := common.SystemName
	common.SystemName = "Gateway"
	t.Cleanup(func() { common.SystemName = previousSystemName })
	send := func(handler gin.HandlerFunc, path, acceptLanguage string) {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		if acceptLanguage != "" {
			c.Request.Header.Set("Accept-Language", acceptLanguage)
		}
		handler(c)
		require.Contains(t, response.Body.String(), `"success":true`, path)
	}
	lastMail := func(receiver string) string {
		mailbox.mutex.Lock()
		defer mailbox.mutex.Unlock()
		mails := mailbox.mail[receiver]
		require.NotEmpty(t, mails, receiver)
		return mails[len(mails)-1]
	}

	send(SendEmailVerification, "/api/verification?email=new@example.com", "zh-CN")
	assert.Contains(t, lastMail("new@example.com"), "您好，你正在进行Gateway邮箱验证。")
	send(SendEmailVerification, "/api/verification?email=new@example.com", "")
	assert.Contains(t, lastMail("new@example.com"), "Hello, you are verifying your email address for Gateway.")

	require.NoError(t, model.DB.Model(user).Updates(map[string]any{"email": "reader@example.com", "setting": `{"language":"zh-CN"}`}).Error)
	send(SendPasswordResetEmail, "/api/reset_password?email=reader@example.com", "en-US")
	assert.Contains(t, lastMail("reader@example.com"), "您好，你正在进行Gateway密码重置。")

	require.NoError(t, model.DB.Model(user).Update("setting", "").Error)
	send(SendPasswordResetEmail, "/api/reset_password?email=reader@example.com", "zh-TW")
	assert.Contains(t, lastMail("reader@example.com"), "您好，您正在進行Gateway密碼重設。")
	send(SendPasswordResetEmail, "/api/reset_password?email=reader@example.com", "")
	assert.Contains(t, lastMail("reader@example.com"), "Hello, you are resetting your password for Gateway.")
}
