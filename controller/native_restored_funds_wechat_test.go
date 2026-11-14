//go:build known_upstream_restored

package controller

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredNativeWeChatStorageErrorStaysPrivate(t *testing.T) {
	db := restoredFundsDB(t)
	oldEnabled, oldRegister, oldQuota := common.WeChatAuthEnabled, common.RegisterEnabled, common.QuotaForNewUser
	oldAddress, oldToken := common.WeChatServerAddress, common.WeChatServerToken
	common.WeChatAuthEnabled, common.RegisterEnabled, common.QuotaForNewUser = true, true, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":"restored-wechat-user"}`))
	}))
	defer server.Close()
	common.WeChatServerAddress, common.WeChatServerToken = server.URL, "test-only"
	t.Cleanup(func() {
		common.WeChatAuthEnabled, common.RegisterEnabled, common.QuotaForNewUser = oldEnabled, oldRegister, oldQuota
		common.WeChatServerAddress, common.WeChatServerToken = oldAddress, oldToken
	})
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("restored_wechat_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(errors.New("private INSERT failure; user state must not escape"))
		}
	}))
	router := gin.New()
	router.GET("/api/oauth/wechat", WeChatAuth)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/oauth/wechat?code=test", nil))
	var body struct {
		Success    bool   `json:"success"`
		Code       string `json:"code"`
		Message    string `json:"message"`
		MessageKey string `json:"message_key"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.False(t, body.Success)
	assert.Equal(t, "AUTH_INTERNAL_ERROR", body.Code)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Equal(t, "Internal Server Error", body.Message)
	assert.Empty(t, body.MessageKey)
	for _, marker := range []string{"private", "INSERT"} {
		assert.False(t, strings.Contains(response.Body.String(), marker), "storage error marker must stay private: %s", marker)
	}
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.Zero(t, count, "rejected registration must not persist a new account")
}
