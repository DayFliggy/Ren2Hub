package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// finishCommittedUserUpdate runs every post-commit action even if an earlier
// action fails. The audit describes the committed mutation, not publication.
func finishCommittedUserUpdate(c *gin.Context, previous, updated *model.User, authzTouched bool, auditParams map[string]any) {
	recordManageAuditFor(c, updated.Id, "user.update", auditParams)
	concurrencyErr := service.RefreshUserConcurrencyLimit(c.Request.Context(), updated.Id)
	if concurrencyErr != nil {
		common.SysError(common.LogText("failed to publish committed user concurrency %d: %s", updated.Id, concurrencyErr.Error()))
	}
	var policyErr, sessionErr error
	if authzTouched {
		policyErr = authz.ReloadPolicy()
	}
	if updated.AuthVersion > previous.AuthVersion {
		_, sessionErr = model.RevokeAllUserSessions(updated.Id, "admin_user_update")
	}
	cacheErr := model.PublishUserAuthCache(updated.Id)
	if cacheErr != nil {
		common.SysError(common.LogText("failed to publish committed user auth cache %d: %s", updated.Id, cacheErr.Error()))
	}
	// Admission retries pending concurrency limits. Cache misses, expiry and
	// auth-version fences cause a database read and republish of the user cache.
	pending := concurrencyErr != nil || cacheErr != nil
	if securityErr := errors.Join(policyErr, sessionErr); securityErr != nil {
		// Policy sync may be disabled, and session cleanup has no durable retry
		// queue. Preserve these errors instead of claiming they are merely pending.
		common.SysError(common.LogText("committed user security cleanup failed %d: %s", updated.Id, securityErr.Error()))
		common.ApiErrorStatus(c, http.StatusOK, securityErr, gin.H{"committed": true, "publication_pending": pending})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "publication_pending": pending})
}
