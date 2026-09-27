package model

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	legacyAuditBackfillMarker = "audit.migration.legacy_logs_v1"
	legacyAuditBackfillBatch  = 500
)

// legacyAuditOther mirrors the retired logs.other metadata of login (7),
// manage (3) and system (4) rows.
type legacyAuditOther struct {
	Op *struct {
		Action string          `json:"action"`
		Params json.RawMessage `json:"params"`
	} `json:"op"`
	AdminInfo *struct {
		AdminID       int    `json:"admin_id"`
		AdminUsername string `json:"admin_username"`
		AdminRole     int    `json:"admin_role"`
		AuthMethod    string `json:"auth_method"`
	} `json:"admin_info"`
	AuditInfo *struct {
		Method     string `json:"method"`
		Route      string `json:"route"`
		Path       string `json:"path"`
		Status     int    `json:"status"`
		Success    *bool  `json:"success"`
		UserAgent  string `json:"user_agent"`
		AuthMethod string `json:"auth_method"`
		ActorRole  int    `json:"actor_role"`
	} `json:"audit_info"`
	LoginMethod string `json:"login_method"`
	UserAgent   string `json:"user_agent"`
	UserRole    int    `json:"user_role"`
}

// BackfillLegacyAuditLogs copies retired login/manage/system rows from logs
// into audit_logs once. Source rows are left in place. Event IDs derive from the
// source row, so an interrupted run can be repeated without duplicates; the
// main-database marker only avoids rescanning logs on every startup.
func BackfillLegacyAuditLogs() error {
	var marker Option
	if err := DB.Where(&Option{Key: legacyAuditBackfillMarker}).Take(&marker).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	legacyTypes := []int{LogTypeManage, LogTypeSystem, LogTypeLogin}
	columns := []string{"id", "user_id", "username", "created_at", "type", "content", "ip", "request_id", "other"}
	clickHouse := common.UsingLogDatabase(common.DatabaseTypeClickHouse)
	lastId, offset, migrated := 0, 0, 0
	for {
		var rows []*Log
		query := LOG_DB.Model(&Log{}).Select(columns).Where("type IN ?", legacyTypes).Limit(legacyAuditBackfillBatch)
		if clickHouse {
			// ClickHouse rows have no unique id. Order by every event ID input, so
			// rows tied across a page boundary share one event ID and are deduplicated.
			query = query.Order("created_at, request_id, user_id, type, content").Offset(offset)
		} else {
			query = query.Where("id > ?", lastId).Order("id")
		}
		if err := query.Find(&rows).Error; err != nil {
			return fmt.Errorf("read legacy audit logs: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		inserted, err := backfillLegacyAuditRows(rows)
		if err != nil {
			return err
		}
		migrated += inserted
		offset += len(rows)
		lastId = rows[len(rows)-1].Id
	}
	if migrated > 0 {
		common.SysLog(fmt.Sprintf("backfilled %d legacy audit logs", migrated))
	}
	return DB.Create(&Option{Key: legacyAuditBackfillMarker, Value: "done"}).Error
}

// backfillLegacyAuditRows converts one batch and inserts entries whose event
// IDs are not yet stored. ClickHouse has no unique index, so the existence
// check is the idempotency guard on every store.
func backfillLegacyAuditRows(rows []*Log) (int, error) {
	entries := make([]AuditLog, 0, len(rows))
	eventIds := make([]string, 0, len(rows))
	ownerIds := make([]int, 0)
	for _, row := range rows {
		entry := legacyAuditEntry(row)
		if entry.Category == AuditCategorySystem && entry.ActorRole == 0 {
			ownerIds = append(ownerIds, entry.UserId)
		}
		entries = append(entries, entry)
		eventIds = append(eventIds, entry.EventId)
	}
	if len(ownerIds) > 0 {
		// Old reward rows carry no role. They are the recipient's own events,
		// so the recipient's role keeps them visible to that recipient.
		var owners []User
		if err := DB.Unscoped().Select("id", "role").Where("id IN ?", ownerIds).Find(&owners).Error; err != nil {
			return 0, fmt.Errorf("read legacy audit owners: %w", err)
		}
		roles := make(map[int]int, len(owners))
		for _, owner := range owners {
			roles[owner.Id] = owner.Role
		}
		for i := range entries {
			if entries[i].Category == AuditCategorySystem && entries[i].ActorRole == 0 && validAuditActorRole(roles[entries[i].UserId]) {
				entries[i].ActorRole = roles[entries[i].UserId]
			}
		}
	}
	var existing []string
	if err := LOG_DB.Model(&AuditLog{}).Where("event_id IN ?", eventIds).Pluck("event_id", &existing).Error; err != nil {
		return 0, fmt.Errorf("read backfilled audit logs: %w", err)
	}
	seen := make(map[string]bool, len(existing)+len(entries))
	for _, eventId := range existing {
		seen[eventId] = true
	}
	pending := make([]AuditLog, 0, len(entries))
	for _, entry := range entries {
		if !seen[entry.EventId] {
			seen[entry.EventId] = true
			pending = append(pending, entry)
		}
	}
	if err := insertAuditLogs(pending); err != nil {
		return 0, fmt.Errorf("write backfilled audit logs: %w", err)
	}
	return len(pending), nil
}

// legacyAuditEntry maps a retired logs row to the audit_logs model. Unknown
// actor roles stay 0 (root only). Unparseable metadata is kept as root-only
// raw text instead of being dropped.
func legacyAuditEntry(row *Log) AuditLog {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%d\x00%d\x00%s\x00%s", row.Id, row.CreatedAt, row.UserId, row.Type, row.RequestId, row.Content)))
	entry := AuditLog{
		EventId: fmt.Sprintf("legacy-log:%x", digest[:24]), UserId: row.UserId, Username: truncateAuditText(row.Username, 64),
		CreatedAt: row.CreatedAt, Content: row.Content, Ip: truncateAuditText(row.Ip, 64),
		RequestId: truncateAuditText(row.RequestId, 64), Status: 200, Success: true,
	}
	switch row.Type {
	case LogTypeLogin:
		entry.Category = AuditCategoryLogin
	case LogTypeManage:
		entry.Category = AuditCategoryOperation
	default:
		entry.Category = AuditCategorySystem
	}
	if strings.TrimSpace(row.Other) == "" {
		return entry
	}
	var legacy legacyAuditOther
	if err := common.UnmarshalJsonStr(row.Other, &legacy); err != nil {
		entry.Other.RootInfo = AuditFields{"legacy_other": row.Other}
		return entry
	}
	// user_role describes the row owner. Only login rows are owned by their
	// actor; on manage rows the owner is the target user.
	actorRole := 0
	if row.Type == LogTypeLogin {
		actorRole = legacy.UserRole
	}
	if legacy.Op != nil {
		entry.Action = truncateAuditText(legacy.Op.Action, 128)
		var params AuditFields
		if err := common.Unmarshal(legacy.Op.Params, &params); err != nil {
			params = nil
		}
		entry.Other.Op = &AuditOperation{Action: entry.Action, Params: sanitizeAuditParams(params)}
	}
	if info := legacy.AuditInfo; info != nil {
		entry.Method, entry.Route = truncateAuditText(info.Method, 16), truncateAuditText(info.Route, 255)
		if info.Status > 0 {
			entry.Status = info.Status
		}
		if info.Success != nil {
			entry.Success = *info.Success
		}
		entry.AuthMethod, entry.UserAgent = info.AuthMethod, info.UserAgent
		if info.ActorRole != 0 {
			actorRole = info.ActorRole
		}
		entry.Other.AuditInfo = &AuditRequestInfo{Method: info.Method, Route: info.Route, Path: info.Path, Status: entry.Status, Success: entry.Success}
	}
	if admin := legacy.AdminInfo; admin != nil {
		if admin.AuthMethod != "" {
			entry.AuthMethod = admin.AuthMethod
		}
		if admin.AdminRole != 0 {
			actorRole = admin.AdminRole
		}
		// Upstream manage rows belonged to the target user. Audit rows belong
		// to the operator, with the target kept in params.
		if admin.AdminID > 0 && admin.AdminID != row.UserId {
			if entry.Other.Op == nil {
				entry.Other.Op = &AuditOperation{}
			}
			if entry.Other.Op.Params == nil {
				entry.Other.Op.Params = AuditFields{}
			}
			entry.Other.Op.Params["target_user_id"] = row.UserId
			entry.UserId, entry.Username = admin.AdminID, truncateAuditText(admin.AdminUsername, 64)
		}
		entry.Other.AdminInfo = &AuditAdminInfo{AdminID: admin.AdminID, AdminUsername: admin.AdminUsername, AdminRole: admin.AdminRole, AuthMethod: admin.AuthMethod}
	}
	if legacy.UserAgent != "" {
		entry.UserAgent = legacy.UserAgent
	}
	entry.AuthMethod, entry.UserAgent = truncateAuditText(entry.AuthMethod, 24), truncateAuditText(entry.UserAgent, 512)
	entry.Other.LoginMethod, entry.Other.UserAgent = legacy.LoginMethod, truncateAuditText(legacy.UserAgent, 512)
	if validAuditActorRole(actorRole) {
		entry.ActorRole = actorRole
	}
	return entry
}
