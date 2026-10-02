package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func passwordResetVerificationRequest(engine *gin.Engine, email, token string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/user/reset", strings.NewReader(`{"email":"`+email+`","token":"`+token+`"}`)))
	return response
}

func TestPasswordResetConsumesTokenBeforeConcurrentAccountWrites(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	const email, token = "one-reset@example.com", "one-time-reset-token"
	require.NoError(t, model.DB.Model(user).Update("email", email).Error)
	common.RegisterVerificationCodeWithKey(email, token, common.PasswordResetPurpose)
	t.Cleanup(func() { common.DeleteKey(email, common.PasswordResetPurpose) })
	engine := gin.New()
	engine.POST("/api/user/reset", ResetPassword)
	entered, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("pause_first_reset_lookup", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && first.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Query().Remove("pause_first_reset_lookup")) })
	firstResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstResponse <- passwordResetVerificationRequest(engine, email, token) }()
	<-entered
	secondResponse := passwordResetVerificationRequest(engine, email, token)
	close(release)
	response := <-firstResponse
	require.Equal(t, http.StatusOK, response.Code)
	var success struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &success))
	require.True(t, success.Success, response.Body.String())
	require.NotEmpty(t, success.Data)
	assert.Contains(t, secondResponse.Body.String(), `"success":false`)
	assert.NotContains(t, secondResponse.Body.String(), token)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.True(t, common.ValidatePasswordAndHash(success.Data, stored.Password))
	assert.Equal(t, user.AuthVersion+1, stored.AuthVersion)
	session, err := model.GetUserSessionBySID(identity.SessionID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusRevoked, session.Status)
	assert.False(t, common.VerifyCodeWithKey(email, token, common.PasswordResetPurpose))
}

func TestPasswordResetFailedWriteDoesNotRestoreConsumedToken(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	const email, token = "failed-reset@example.com", "failed-write-reset-token"
	require.NoError(t, model.DB.Model(user).Update("email", email).Error)
	common.RegisterVerificationCodeWithKey(email, token, common.PasswordResetPurpose)
	t.Cleanup(func() { common.DeleteKey(email, common.PasswordResetPurpose) })
	engine := gin.New()
	engine.POST("/api/user/reset", ResetPassword)
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("reject_reset_write", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("reset write rejected"))
		}
	}))
	response := passwordResetVerificationRequest(engine, email, token)
	require.NoError(t, model.DB.Callback().Update().Remove("reject_reset_write"))
	assert.Contains(t, response.Body.String(), `"success":false`)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, user.Password, stored.Password)
	assert.Equal(t, user.AuthVersion, stored.AuthVersion)
	response = passwordResetVerificationRequest(engine, email, token)
	assert.Contains(t, response.Body.String(), `"success":false`, "the caller must request a new link after a failed reset")
	assert.NotContains(t, response.Body.String(), token)
	assert.False(t, common.VerifyCodeWithKey(email, token, common.PasswordResetPurpose))
}
