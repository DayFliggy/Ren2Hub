package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func RecordAccountLoginLog(userId, actorRole int, username string, content string, ip string, action string, params map[string]any, other AuditOther, request ...*gin.Context) {
	other.Op = &AuditOperation{Action: action, Params: params}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	RecordAuditLog(c, AuditLog{UserId: userId, Username: username, ActorRole: actorRole, Category: AuditCategoryLogin, Action: action, Content: content, Ip: ip, Other: other, Success: true})
}

// RecordAccountSystemAuditLog records a system-granted event owned by its
// recipient. The recipient's role is the actor role, so the event is visible to
// that user and follows the same role boundary as other audit records. request
// is optional because registration rewards are granted outside a request.
func RecordAccountSystemAuditLog(userId int, content string, action string, params map[string]any, request ...*gin.Context) {
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	entry := AuditLog{UserId: userId, Category: AuditCategorySystem, Action: action, Content: content, Status: 200, Success: true, Other: AuditOther{Op: &AuditOperation{Action: action, Params: params}}}
	var owner User
	if err := DB.Unscoped().Select("id", "username", "role").Where("id = ?", userId).Take(&owner).Error; err != nil {
		common.SysError(fmt.Sprintf("system audit owner lookup failed (user_id=%d, action=%s): %v", userId, action, err))
	} else {
		entry.Username, entry.ActorRole = owner.Username, owner.Role
	}
	RecordAuditLog(c, entry)
}

// RecordAccountOperationAuditLog writes new operation/security events to the audit table.
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（供导出使用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordAccountOperationAuditLog(logUserId, actorRole int, content string, ip string, action string, params map[string]any, adminInfo *AuditAdminInfo, auditInfo *AuditRequestInfo, request ...*gin.Context) {
	username, _ := GetUsernameById(logUserId, false)
	other := AuditOther{
		Op:        &AuditOperation{Action: action, Params: params},
		AdminInfo: adminInfo,
		AuditInfo: auditInfo,
	}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	category := AuditCategoryOperation
	if adminInfo == nil {
		category = AuditCategorySecurity
	}
	status, success := 200, true
	if auditInfo != nil {
		status = auditInfo.Status
		success = auditInfo.Success
	}
	RecordAuditLog(c, AuditLog{UserId: logUserId, Username: username, ActorRole: actorRole, Category: category, Action: action, Content: content, Ip: ip, Status: status, Success: success, Other: other})
}
