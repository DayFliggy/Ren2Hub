package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"gorm.io/gorm"
)

var patScopePermissions = map[string]authz.Permission{
	"channels:read": authz.ChannelRead, "channels:write": authz.ChannelWrite,
	"channels:operate": authz.ChannelOperate, "channels:sensitive_write": authz.ChannelSensitiveWrite,
	"channels:key_read": authz.ChannelSecretView, "audit:read": authz.AuditRead,
	"tickets:read": authz.TicketRead, "tickets:reply": authz.TicketReply, "tickets:manage": authz.TicketManage,
	"plugins:bind": authz.TaskPluginBind,
}

func PATScopeForPermission(permission authz.Permission) (string, bool) {
	for scope, mapped := range patScopePermissions {
		if mapped == permission {
			return scope, true
		}
	}
	return "", false
}

func ValidatePATScopesWithTx(tx *gorm.DB, owner *model.User, scopes []string) error {
	for _, scope := range scopes {
		minimum, known := model.PATScopeRoles[scope]
		if !known || owner.Role < minimum {
			return ErrVerificationForbidden
		}
		if permission, ok := patScopePermissions[scope]; ok {
			allowed, err := authz.CanWithDB(tx, owner.Id, owner.Role, permission)
			if err != nil {
				return err
			}
			if !allowed {
				return ErrVerificationForbidden
			}
		}
	}
	return nil
}

// CheckPATScope intersects a stored grant with current role and permissions;
// editing the owner's authorization never leaves a more privileged PAT behind.
func CheckPATScope(userID, role int, scope string) (bool, error) {
	minimum, known := model.PATScopeRoles[scope]
	if !known || role < minimum {
		return false, nil
	}
	if permission, ok := patScopePermissions[scope]; ok {
		return authz.CanWithDB(model.DB, userID, role, permission)
	}
	return true, nil
}

func ValidateSecurityIdentity(identity AuthIdentity) error {
	if identity.PersonalAccessTokenID > 0 {
		return model.ValidateAuthSessionWithTx(model.DB, identity)
	}
	_, _, err := ValidateLoginSession(identity)
	return err
}
