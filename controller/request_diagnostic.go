package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetSelfRequestDiagnostic(c *gin.Context)    { getRequestDiagnostic(c, true) }
func GetAdminRequestDiagnostic(c *gin.Context)   { getRequestDiagnostic(c, false) }
func ListSelfRequestDiagnostics(c *gin.Context)  { listRequestDiagnostics(c, true) }
func ListAdminRequestDiagnostics(c *gin.Context) { listRequestDiagnostics(c, false) }

func getRequestDiagnostic(c *gin.Context, self bool) {
	requestID := c.Param("request_id")
	if !service.ValidRequestCorrelationID(requestID) || len(requestID) > 64 {
		common.ApiErrorMsg(c, "Invalid request identifier")
		return
	}
	userID := 0
	if self {
		userID = c.GetInt("id")
		if userID <= 0 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
	}
	row, err := model.GetGatewayRequestDiagnostic(c.Request.Context(), requestID, userID, time.Now().Add(-service.RequestDiagnosticRetentionDays*24*time.Hour).Unix())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "REQUEST_DIAGNOSTIC_UNAVAILABLE", "message": "Request diagnostics are unavailable or expired."})
		return
	}
	if err != nil {
		_ = c.Error(err)
		c.JSON(500, gin.H{"success": false, "message": "Diagnostic storage unavailable."})
		return
	}
	internal := false
	if !self {
		internal, err = middleware.CurrentUserPermission(c, authz.ChannelRead)
		if err != nil {
			_ = c.Error(err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
	}
	result, err := service.ProjectRequestDiagnostic(*row, internal)
	if err != nil {
		_ = c.Error(err)
		c.JSON(500, gin.H{"success": false, "message": "Diagnostic data unavailable."})
		return
	}
	common.ApiSuccess(c, result)
}

func listRequestDiagnostics(c *gin.Context, self bool) {
	filter := model.RequestDiagnosticFilter{RequestID: c.Query("request_id"), TraceID: c.Query("trace_id"), ThreadID: c.Query("thread_id")}
	for _, value := range []string{filter.RequestID, filter.TraceID, filter.ThreadID} {
		if value != "" && !service.ValidRequestCorrelationID(value) {
			c.JSON(400, gin.H{"success": false, "message": "Invalid correlation identifier."})
			return
		}
	}
	if raw := c.Query("token_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"success": false, "message": "Invalid token ID."})
			return
		}
		filter.TokenID = id
	}
	if (filter.TraceID != "" || filter.ThreadID != "") && filter.TokenID <= 0 {
		c.JSON(400, gin.H{"success": false, "message": "A token ID is required for correlation queries."})
		return
	}
	if self {
		filter.UserID = c.GetInt("id")
		if filter.UserID <= 0 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
	}
	page, size := common.GetPageQuery(c).Page, common.GetPageQuery(c).PageSize
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		c.JSON(400, gin.H{"success": false, "message": "Invalid diagnostic pagination."})
		return
	}
	rows, total, err := model.QueryGatewayRequestDiagnostics(c.Request.Context(), filter, (page-1)*size, size, time.Now().Add(-service.RequestDiagnosticRetentionDays*24*time.Hour).Unix())
	if err != nil {
		_ = c.Error(err)
		c.JSON(500, gin.H{"success": false, "message": "Diagnostic storage unavailable."})
		return
	}
	internal := false
	if !self {
		internal, err = middleware.CurrentUserPermission(c, authz.ChannelRead)
		if err != nil {
			_ = c.Error(err)
			c.AbortWithStatus(500)
			return
		}
	}
	items := make([]service.RequestDiagnosticView, 0, len(rows))
	for _, row := range rows {
		item, err := service.ProjectRequestDiagnostic(row, internal)
		if err != nil {
			_ = c.Error(err)
			c.JSON(500, gin.H{"success": false, "message": "Diagnostic data unavailable."})
			return
		}
		items = append(items, item)
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}
