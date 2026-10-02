package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManagedUserEntrypointsRejectUnknownRolesWithoutWrites(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	for _, role := range []int{-1, 0, 2, 9, 11, 99, 101} {
		for _, handler := range []gin.HandlerFunc{CreateUser, NextCreateAdminUser} {
			body := fmt.Sprintf(`{"username":"invalid-role","password":"long-enough-password","role":%d,"status":1}`, role)
			response := securityEnrollmentRequest("POST", "/api/user/", body, "", identity, func(c *gin.Context) { c.Set("role", common.RoleRootUser); handler(c) })
			assert.Contains(t, response.Body.String(), `"success":false`)
			var count int64
			require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "invalid-role").Count(&count).Error)
			assert.Zero(t, count)
		}
	}
}

func TestAdminSensitiveWriteRejectsMissingExpiredAndWrongTargetProof(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	target := model.User{Username: "managed-target", AffCode: "managed-target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(&target).Error)
	for _, failure := range []string{"missing", "expired", "wrong-target"} {
		t.Run(failure, func(t *testing.T) {
			proof := ""
			id := target.Id
			if failure == "wrong-target" {
				id++
			}
			if failure != "missing" {
				proof = issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeAdminUserChange, Context: []byte(fmt.Sprintf(`{"ids":[%d],"action":"disable"}`, id))}, "password")
			}
			if failure == "expired" {
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeSecurityProof).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			}
			response := securityEnrollmentRequest("POST", "/api/next/admin/users/status/batch", fmt.Sprintf(`{"ids":[%d],"status":2}`, target.Id), proof, identity, func(c *gin.Context) { c.Set("role", common.RoleRootUser); NextAdminUserStatusBatch(c) })
			require.Equal(t, http.StatusForbidden, response.Code)
			var stored model.User
			require.NoError(t, model.DB.First(&stored, target.Id).Error)
			assert.Equal(t, common.UserStatusEnabled, stored.Status)
			assert.EqualValues(t, 1, stored.AuthVersion)
		})
	}
}

func TestPATStepUpIsBoundToCredentialAndRevocation(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	token, _, err := model.CreatePersonalAccessToken(identity, "admin", []string{"users:write", "verification:write"}, 0)
	require.NoError(t, err)
	patIdentity := service.AuthIdentity{UserID: user.Id, UserAuthVersion: identity.UserAuthVersion, SessionID: fmt.Sprintf("pat:%d", token.ID), SessionVersion: 1, PersonalAccessTokenID: token.ID}
	operation := service.VerificationOperation{Scope: service.VerificationScopeAdminUserChange, Context: []byte(`{"ids":[2],"action":"disable"}`)}
	proof, err := service.VerifySecurityInput(patIdentity, service.VerificationInput{Scope: operation.Scope, Context: operation.Context, Method: "password", Password: "enrollment-password"})
	require.NoError(t, err)
	_, err = service.ConsumeOperationProof(proof.ProofToken, identity, operation)
	require.ErrorIs(t, err, service.ErrAuthTokenInvalid)
	_, err = service.ConsumeOperationProof(proof.ProofToken, patIdentity, operation)
	require.NoError(t, err)
	_, err = service.ConsumeOperationProof(proof.ProofToken, patIdentity, operation)
	require.ErrorIs(t, err, service.ErrProofConsumed)
	_, err = service.GetVerificationRequirements(patIdentity, service.VerificationScopePATCreate)
	require.ErrorIs(t, err, service.ErrVerificationForbidden)
	proof, err = service.VerifySecurityInput(patIdentity, service.VerificationInput{Scope: operation.Scope, Context: operation.Context, Method: "password", Password: "enrollment-password"})
	require.NoError(t, err)
	require.NoError(t, model.RevokePersonalAccessToken(identity, token.ID))
	_, err = service.ConsumeOperationProof(proof.ProofToken, patIdentity, operation)
	require.Error(t, err)
}

func TestAdminEmailChangeRequiresProofAndInvalidatesSessions(t *testing.T) {
	db := setupManageUserTestDB(t)
	target := model.User{Username: "email-target", AffCode: "email-target", Email: "original@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(&target).Error)
	body := fmt.Sprintf(`{"id":%d,"username":"email-target","display_name":"Target","email":"changed@example.com","role":1,"status":1}`, target.Id)
	response := securityEnrollmentRequest("PUT", "/api/next/admin/users", body, "", service.AuthIdentity{UserID: 9999, SessionID: "unverified", UserAuthVersion: 1, SessionVersion: 1}, func(c *gin.Context) { c.Set("role", common.RoleRootUser); NextUpdateAdminUser(c) })
	require.Equal(t, http.StatusForbidden, response.Code)
	var stored model.User
	require.NoError(t, db.First(&stored, target.Id).Error)
	assert.Equal(t, "original@example.com", stored.Email)
	assert.EqualValues(t, 1, stored.AuthVersion)
	response = performNextAdminJSON(t, "PUT", "/api/next/admin/users", body, NextUpdateAdminUser)
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&stored, target.Id).Error)
	assert.Equal(t, "changed@example.com", stored.Email)
	assert.EqualValues(t, 2, stored.AuthVersion)
}

func TestAdminProfileWriteCannotRestoreConcurrentlyChangedSecurityState(t *testing.T) {
	for _, field := range []string{"status", "role", "email"} {
		t.Run(field, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			target := model.User{Username: "concurrent-target", AffCode: "concurrent-target", Email: "original@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
			require.NoError(t, db.Create(&target).Error)
			changed := false
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:concurrent-security-update", func(tx *gorm.DB) {
				snapshot, ok := tx.Statement.Dest.(*model.User)
				if changed || !ok || snapshot.Id != target.Id {
					return
				}
				changed = true
				var value any = common.UserStatusDisabled
				if field == "role" {
					value = common.RoleAdminUser
				}
				if field == "email" {
					value = "other@example.com"
				}
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", target.Id).Updates(map[string]any{field: value, "auth_version": 2}).Error)
			}))
			body := fmt.Sprintf(`{"id":%d,"username":"concurrent-target","display_name":"edited","email":"original@example.com","role":1,"status":1}`, target.Id)
			response := securityEnrollmentRequest("PUT", "/api/next/admin/users", body, "", service.AuthIdentity{}, func(c *gin.Context) { c.Set("role", common.RoleRootUser); NextUpdateAdminUser(c) })
			require.True(t, changed)
			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), "SECURITY_PROOF_REQUIRED")
			var stored model.User
			require.NoError(t, db.First(&stored, target.Id).Error)
			assert.Empty(t, stored.DisplayName)
			assert.EqualValues(t, 2, stored.AuthVersion)
		})
	}
}

func TestLegacyAdminEditRechecksTargetRoleUnderLock(t *testing.T) {
	db := setupManageUserTestDB(t)
	target := model.User{Username: "promoted-target", AffCode: "promoted-target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, Password: "original-hash"}
	require.NoError(t, db.Create(&target).Error)
	snapshot := target
	require.NoError(t, db.Model(&target).Update("role", common.RoleAdminUser).Error)
	snapshot.Password = "new-long-password"
	err := db.Transaction(func(tx *gorm.DB) error { return snapshot.EditWithTx(tx, true, common.RoleAdminUser) })
	require.ErrorIs(t, err, model.ErrManagedUserForbidden)
	var stored model.User
	require.NoError(t, db.First(&stored, target.Id).Error)
	assert.Equal(t, "original-hash", stored.Password)
	assert.EqualValues(t, 1, stored.AuthVersion)
}
