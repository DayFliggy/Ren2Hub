package model

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ListLogGroups returns only values already visible in the caller's log scope.
// Administrative callers pass zero; self endpoints must pass their verified ID.
func ListLogGroups(ctx context.Context, userID int, search string) ([]string, error) {
	column := "`group`"
	if common.UsingLogDatabase(common.DatabaseTypePostgreSQL) {
		column = `"group"`
	}
	query := LOG_DB.WithContext(ctx).Model(&Log{}).Where(column+" <> ?", "")
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if search = strings.TrimSpace(search); search != "" {
		condition, value := buildLiteralLogContainsCondition(column, search)
		query = query.Where(condition, value)
	}
	groups := make([]string, 0)
	err := query.Distinct(column).Order(column).Limit(100).Pluck(column, &groups).Error
	return groups, err
}
