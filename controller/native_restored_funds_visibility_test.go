//go:build known_upstream_restored

package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredNativeAdminListHidesRoot(t *testing.T) {
	db := restoredFundsDB(t)
	for _, u := range []model.User{{Id: 7101, Username: "diag-root", Role: common.RoleRootUser}, {Id: 7102, Username: "diag-admin", Role: common.RoleAdminUser}, {Id: 7103, Username: "diag-user", Role: common.RoleCommonUser}} {
		u.AffCode = u.Username
		require.NoError(t, db.Create(&u).Error)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/?page=1&page_size=10", nil)
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 7102)
	GetAllUsers(c)
	var result struct {
		Success bool
		Data    struct {
			Total int
			Items []model.User
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Success)
	for _, u := range result.Data.Items {
		assert.Less(t, u.Role, common.RoleAdminUser, "restored_native_admin_visibility: root and peer administrator must remain hidden")
		assert.Empty(t, u.Password)
	}
	assert.Equal(t, 1, result.Data.Total, "restored_native_admin_count")
}
func TestRestoredNativeCommittedCacheFailureHasRecoveryFeedback(t *testing.T) {
	db := restoredFundsDB(t)
	require.NoError(t, authz.Init(db))
	server := miniredis.RunT(t)
	oldRDB := common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() { client.Close(); common.RDB = oldRDB })
	u := model.User{Id: 7104, Username: "diag-cache", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&u).Error)
	committed := false
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("diag:commit", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			committed = true
		}
	}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("diag:cache-failure", func(tx *gorm.DB) {
		if committed && tx.Statement.ConnPool == db.Statement.ConnPool && tx.Statement.Table == "users" && len(tx.Statement.Selects) != 2 {
			tx.AddError(errors.New("diagnostic auth cache publication unavailable"))
		}
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", strings.NewReader(fmt.Sprintf(`{"id":%d,"username":%q,"display_name":"Updated"}`, u.Id, u.Username)))
	c.Set("role", common.RoleRootUser)
	c.Set("id", 9999)
	UpdateUser(c)
	t.Logf("response=%s", w.Body.String())
	require.True(t, committed)
	var result struct {
		Success bool
		Pending bool `json:"publication_pending"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result.Success, "restored_native_cache_failure_success_feedback")
	assert.True(t, result.Pending, "restored_native_cache_failure_pending_feedback")
}
