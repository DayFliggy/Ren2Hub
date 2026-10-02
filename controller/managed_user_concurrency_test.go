package controller

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManagedUserActionPreservesConcurrentSecurityAndWalletChanges(t *testing.T) {
	for _, action := range []string{"enable", "disable", "promote", "demote"} {
		t.Run(action, func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			db := model.DB
			require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.Token{}))
			require.NoError(t, authz.Init(db))
			if action == "demote" {
				require.NoError(t, db.Model(user).Update("role", common.RoleAdminUser).Error)
			}
			changed := false
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:managed_security_interleave", func(tx *gorm.DB) {
				snapshot, ok := tx.Statement.Dest.(*model.User)
				if changed || !ok || snapshot.Id != user.Id {
					return
				}
				changed = true
				updates := map[string]any{
					"password": "new-password-hash", "auth_version": 2, "email": "new@example.com",
					"github_id": "new-binding", "group": "new-group", "available_quota": 987,
					"reserved_quota": 123, "quota_version": 4,
				}
				if action == "promote" || action == "demote" {
					updates["status"] = common.UserStatusDisabled
				}
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Updates(updates).Error)
			}))
			t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove("test:managed_security_interleave")) })
			response := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":%q}`, user.Id, action))
			require.Contains(t, response.Body.String(), `"success":true`)
			require.True(t, changed)
			var stored model.User
			require.NoError(t, db.First(&stored, user.Id).Error)
			assert.Equal(t, "new-password-hash", stored.Password)
			assert.Equal(t, "new@example.com", stored.Email)
			assert.Equal(t, "new-binding", stored.GitHubId)
			assert.Equal(t, "new-group", stored.Group)
			assert.EqualValues(t, 987, stored.Quota)
			assert.EqualValues(t, 123, stored.ReservedQuota)
			assert.EqualValues(t, 4, stored.QuotaVersion)
			expectedVersion := int64(3)
			expectedStatus, expectedRole := common.UserStatusDisabled, common.RoleCommonUser
			if action == "enable" {
				expectedVersion, expectedStatus = 2, common.UserStatusEnabled
			}
			if action == "promote" {
				expectedRole = common.RoleAdminUser
			}
			assert.Equal(t, expectedVersion, stored.AuthVersion)
			assert.Equal(t, expectedStatus, stored.Status)
			assert.Equal(t, expectedRole, stored.Role)
		})
	}
}

func TestManagedUserActionRechecksNewlyPrivilegedTarget(t *testing.T) {
	for _, action := range []string{"disable", "promote", "demote"} {
		t.Run(action, func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			db := model.DB
			if action == "demote" {
				require.NoError(t, db.Model(user).Update("role", common.RoleAdminUser).Error)
			}
			changed := false
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:managed_privilege_interleave", func(tx *gorm.DB) {
				snapshot, ok := tx.Statement.Dest.(*model.User)
				if changed || !ok || snapshot.Id != user.Id {
					return
				}
				changed = true
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]any{"role": common.RoleRootUser, "auth_version": 2}).Error)
			}))
			t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove("test:managed_privilege_interleave")) })
			response := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":%q}`, user.Id, action))
			require.Contains(t, response.Body.String(), `"success":false`)
			var stored model.User
			require.NoError(t, db.First(&stored, user.Id).Error)
			assert.Equal(t, common.RoleRootUser, stored.Role)
			assert.Equal(t, common.UserStatusEnabled, stored.Status)
			assert.EqualValues(t, 2, stored.AuthVersion)
			var audits int64
			require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("action = ?", "user.manage").Count(&audits).Error)
			assert.Zero(t, audits)
		})
	}
}

func TestUserUpdateWithoutPasswordPreservesConcurrentCredential(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	stale, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(user).Updates(map[string]any{"password": "new-password-hash", "auth_version": 2}).Error)
	stale.DisplayName = "Updated profile"
	require.NoError(t, stale.Update(false))
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, "new-password-hash", stored.Password)
	assert.Equal(t, "Updated profile", stored.DisplayName)
	assert.EqualValues(t, 2, stored.AuthVersion)
}

func TestManagedUserDemotionRollsBackWhenPermissionCleanupFails(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.Token{}))
	require.NoError(t, authz.Init(db))
	require.NoError(t, db.Model(user).Update("role", common.RoleAdminUser).Error)
	rule := model.CasbinRule{Ptype: "p", V0: authz.UserSubject(user.Id), V1: "channels", V2: "read", V3: "allow"}
	require.NoError(t, db.Create(&rule).Error)
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:permission_cleanup_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "casbin_rule" {
			tx.AddError(errors.New("permission cleanup unavailable"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Delete().Remove("test:permission_cleanup_failure")) })
	response := performManageUserRequest(t, fmt.Sprintf(`{"id":%d,"action":"demote"}`, user.Id))
	require.Contains(t, response.Body.String(), `"success":false`)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, common.RoleAdminUser, stored.Role)
	assert.EqualValues(t, 1, stored.AuthVersion)
	assert.Equal(t, user.Password, stored.Password)
	var existing model.CasbinRule
	require.NoError(t, db.First(&existing, rule.Id).Error)
	var sessions []model.UserSession
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&sessions).Error)
	require.Len(t, sessions, 1)
	assert.Equal(t, model.UserSessionStatusActive, sessions[0].Status)
	var audits int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Where("action = ?", "user.manage").Count(&audits).Error)
	assert.Zero(t, audits)
}
