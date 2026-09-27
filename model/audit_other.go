package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// AuditOther is the structured metadata stored with an audit event. Privileged
// fields are separate so API projections can remove them without re-encoding JSON.
type AuditOther struct {
	Op          *AuditOperation   `json:"op,omitempty"`
	AdminInfo   *AuditAdminInfo   `json:"admin_info,omitempty"`
	AuditInfo   *AuditRequestInfo `json:"audit_info,omitempty"`
	RootInfo    AuditFields       `json:"root_info,omitempty"`
	LoginMethod string            `json:"login_method,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
}

type AuditOperation struct {
	Action string      `json:"action"`
	Params AuditFields `json:"params,omitempty"`
}

type AuditAdminInfo struct {
	AdminID       int    `json:"admin_id,omitempty"`
	AdminUsername string `json:"admin_username,omitempty"`
	AdminRole     int    `json:"admin_role,omitempty"`
	AuthMethod    string `json:"auth_method,omitempty"`
}

type AuditRequestInfo struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Success bool              `json:"success"`
	Params  map[string]string `json:"params,omitempty"`
}

// AuditFields holds action-specific parameters and root-only extensions.
// Retain their encoded values when reading so arbitrary nested integers do not
// round-trip through float64 and lose precision before the API returns them.
type AuditFields map[string]any

func (fields *AuditFields) UnmarshalJSON(data []byte) error {
	var values map[string]json.RawMessage
	if err := common.Unmarshal(data, &values); err != nil {
		return err
	}
	if values == nil {
		*fields = nil
		return nil
	}
	decoded := make(AuditFields, len(values))
	for key, value := range values {
		decoded[key] = value
	}
	*fields = decoded
	return nil
}

// sanitizeAuditParams removes credential-like keys at any depth. Current
// writers pass explicit safe fields; this also covers legacy rows written
// without that guarantee. Values stay encoded, so integers keep their precision.
func sanitizeAuditParams(params AuditFields) AuditFields {
	if params == nil {
		return nil
	}
	result := make(AuditFields, len(params))
	for key, value := range params {
		if auditSensitiveKey(key) {
			continue
		}
		raw, ok := value.(json.RawMessage)
		if !ok {
			encoded, err := common.Marshal(value)
			if err != nil {
				continue
			}
			raw = encoded
		}
		result[key] = sanitizeAuditRaw(raw)
	}
	return result
}

// sanitizeAuditRaw returns raw unchanged unless a nested object has a
// credential-like key; only then is the affected container re-encoded.
func sanitizeAuditRaw(raw json.RawMessage) json.RawMessage {
	var object map[string]json.RawMessage
	if err := common.Unmarshal(raw, &object); err == nil && object != nil {
		changed := false
		for key, value := range object {
			if auditSensitiveKey(key) {
				delete(object, key)
				changed = true
				continue
			}
			if sanitized := sanitizeAuditRaw(value); !bytes.Equal(sanitized, value) {
				object[key], changed = sanitized, true
			}
		}
		if !changed {
			return raw
		}
		encoded, err := common.Marshal(object)
		if err != nil {
			return json.RawMessage("{}")
		}
		return encoded
	}
	var list []json.RawMessage
	if err := common.Unmarshal(raw, &list); err == nil {
		changed := false
		for i, value := range list {
			if sanitized := sanitizeAuditRaw(value); !bytes.Equal(sanitized, value) {
				list[i], changed = sanitized, true
			}
		}
		if !changed {
			return raw
		}
		encoded, err := common.Marshal(list)
		if err != nil {
			return json.RawMessage("[]")
		}
		return encoded
	}
	return raw
}

func auditSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	if strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") {
		return true
	}
	if normalized == "token" || strings.HasSuffix(normalized, "_token") {
		return true
	}
	switch normalized {
	case "api_key", "apikey", "authorization", "cookie", "set_cookie":
		return true
	default:
		return false
	}
}

func (other AuditOther) Value() (driver.Value, error) {
	data, err := common.Marshal(other)
	if err != nil {
		return nil, err
	}
	// PostgreSQL's simple protocol requires string, not a bytea parameter.
	return string(data), nil
}

func (other *AuditOther) Scan(value any) error {
	var data []byte
	switch value := value.(type) {
	case nil:
		*other = AuditOther{}
		return nil
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("unsupported audit metadata database type %T", value)
	}
	var decoded AuditOther
	if len(data) > 0 {
		if err := common.Unmarshal(data, &decoded); err != nil {
			return err
		}
	}
	*other = decoded
	return nil
}
