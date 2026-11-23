package controller

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type systemReleases interface {
	CheckUpdate(context.Context, bool) (*service.SystemUpdateInfo, error)
	ListRollbackVersions(context.Context) ([]service.RollbackVersion, error)
	PerformUpdate(context.Context) (*service.SystemUpdateInfo, error)
	RollbackToVersion(context.Context, string) error
}

type systemUpdateHandler struct {
	releases  systemReleases
	restart   func() error
	operation sync.Mutex
}

var SystemUpdates = &systemUpdateHandler{
	releases: service.NewSystemUpdateService(service.SystemReleaseAPI, &http.Client{Timeout: 15 * time.Minute}),
	restart:  service.RestartSystemProcess,
}

func (*systemUpdateHandler) Version(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"version": common.Version, "build_type": common.BuildType})
}

func (h *systemUpdateHandler) CheckUpdates(c *gin.Context) {
	info, err := h.releases.CheckUpdate(c.Request.Context(), c.Query("force") == "true")
	if err != nil {
		writeSystemUpdateError(c, err)
		return
	}
	common.ApiSuccess(c, info)
}

func (h *systemUpdateHandler) RollbackVersions(c *gin.Context) {
	versions, err := h.releases.ListRollbackVersions(c.Request.Context())
	if err != nil {
		writeSystemUpdateError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"versions": versions})
}

func (h *systemUpdateHandler) beginOperation(c *gin.Context) bool {
	if h.operation.TryLock() {
		return true
	}
	common.ApiErrorStatus(c, http.StatusConflict, errors.New("another system operation is in progress"))
	return false
}

func systemOperationContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(c.Request.Context()), 15*time.Minute)
}

func (h *systemUpdateHandler) Update(c *gin.Context) {
	if !h.beginOperation(c) {
		return
	}
	defer h.operation.Unlock()
	ctx, cancel := systemOperationContext(c)
	defer cancel()
	info, err := h.releases.PerformUpdate(ctx)
	if err != nil {
		writeSystemUpdateError(c, err)
		return
	}
	recordManageAudit(c, "system.update", map[string]any{"version": info.LatestVersion, "has_update": info.HasUpdate})
	if !info.HasUpdate {
		common.ApiSuccess(c, gin.H{"already_up_to_date": true, "current_version": info.CurrentVersion, "latest_version": info.LatestVersion})
		return
	}
	common.ApiSuccess(c, gin.H{"need_restart": true})
}

func (h *systemUpdateHandler) Rollback(c *gin.Context) {
	if !h.beginOperation(c) {
		return
	}
	defer h.operation.Unlock()
	var request struct {
		Version string `json:"version"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("invalid rollback request"))
		return
	}
	ctx, cancel := systemOperationContext(c)
	defer cancel()
	if err := h.releases.RollbackToVersion(ctx, request.Version); err != nil {
		writeSystemUpdateError(c, err)
		return
	}
	recordManageAudit(c, "system.rollback", map[string]any{"version": request.Version})
	common.ApiSuccess(c, gin.H{"need_restart": true, "version": request.Version})
}

func (h *systemUpdateHandler) Restart(c *gin.Context) {
	if !h.beginOperation(c) {
		return
	}
	recordManageAudit(c, "system.restart", nil)
	common.ApiSuccess(c, gin.H{"restarting": true})
	c.Writer.Flush()
	time.AfterFunc(500*time.Millisecond, func() {
		if err := h.restart(); err != nil {
			common.SysError("failed to signal system restart: " + err.Error())
		}
		h.operation.Unlock()
	})
}

func writeSystemUpdateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSystemSourceBuild):
		common.ApiErrorStatus(c, http.StatusBadRequest, service.ErrSystemSourceBuild)
	case errors.Is(err, service.ErrSystemInvalidVersion):
		common.ApiErrorStatus(c, http.StatusBadRequest, service.ErrSystemInvalidVersion)
	case errors.Is(err, service.ErrSystemRollbackNotFound):
		common.ApiErrorStatus(c, http.StatusNotFound, service.ErrSystemRollbackNotFound)
	case errors.Is(err, service.ErrSystemRelease), errors.Is(err, context.DeadlineExceeded):
		common.ApiErrorStatus(c, http.StatusBadGateway, service.ErrSystemRelease)
	case errors.Is(err, service.ErrSystemReplace):
		common.ApiErrorStatus(c, http.StatusInternalServerError, service.ErrSystemReplace)
	default:
		common.ApiErrorStatus(c, http.StatusInternalServerError, errors.New("system update failed"))
	}
}
