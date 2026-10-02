package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func requireAdminUserProof(c *gin.Context, action string, ids ...int) *model.AuthFlowAuthorization {
	context, err := common.Marshal(service.AdminUserChangeContext{IDs: ids, Action: action})
	if err != nil {
		writeSecurityOperationError(c, err)
		return nil
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAdminUserChange, Context: context})
	if authorization != nil {
		c.Set("admin_user_authorization", authorization.AuthSessionIdentity)
	}
	return authorization
}

func adminUserAuthorization(c *gin.Context) []model.AuthSessionIdentity {
	value, ok := c.Get("admin_user_authorization")
	if !ok {
		return nil
	}
	return []model.AuthSessionIdentity{value.(model.AuthSessionIdentity)}
}

func validateAdminWriteAuthorization(c *gin.Context, tx *gorm.DB) error {
	for _, identity := range adminUserAuthorization(c) {
		if err := model.ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
	}
	return nil
}
