package model

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrInvalidLogTypes       = errors.New("invalid log types")
	ErrInvalidAuditCategory  = errors.New("invalid audit categories")
)

// ValidLogType reports whether logType is one of the persisted log type codes.
func ValidLogType(logType int) bool {
	return logType >= LogTypeTopup && logType <= LogTypeLogin
}

// ParseLogTypesParam parses a comma-separated list of persisted log type codes
// (for example "2,5" for call logs or "1,4,6" for quota ledger entries).
func ParseLogTypesParam(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > LogTypeLogin {
		return nil, ErrInvalidLogTypes
	}
	seen := make(map[int]struct{}, len(parts))
	types := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, ErrInvalidLogTypes
		}
		code, err := strconv.Atoi(part)
		if err != nil || !ValidLogType(code) {
			return nil, ErrInvalidLogTypes
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		types = append(types, code)
	}
	if len(types) == 0 {
		return nil, ErrInvalidLogTypes
	}
	return types, nil
}

// ParseAuditCategoriesParam parses a comma-separated list of audit categories
// (for example "login,security").
func ParseAuditCategoriesParam(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 4 {
		return nil, ErrInvalidAuditCategory
	}
	seen := make(map[string]struct{}, len(parts))
	categories := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !ValidAuditCategory(part) {
			return nil, ErrInvalidAuditCategory
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		categories = append(categories, part)
	}
	if len(categories) == 0 {
		return nil, ErrInvalidAuditCategory
	}
	return categories, nil
}
