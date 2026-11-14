package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// finishCommittedUserUpdate publishes the local concurrency limit before the
// native security side effects, whose error ordering remains unchanged.
func finishCommittedUserUpdate(c *gin.Context, previous, updated *model.User, authzTouched bool, auditParams map[string]any) {
	concurrencyErr := service.RefreshUserConcurrencyLimit(c.Request.Context(), updated.Id)
	if concurrencyErr != nil {
		common.SysError(common.LogText("failed to publish committed user concurrency %d: %s", updated.Id, concurrencyErr.Error()))
	}
	if authzTouched {
		model.BumpSyncTopic(model.SyncTopicAuthz)
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if updated.AuthVersion > previous.AuthVersion {
		if _, err := model.RevokeAllUserSessions(updated.Id, "admin_user_update"); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := model.PublishUserAuthCache(updated.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, updated.Id, "user.update", auditParams)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "publication_pending": concurrencyErr != nil})
}
