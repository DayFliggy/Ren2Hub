//go:build known_upstream_restored

package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This fixture uses only fields shared by b388 and Ren2Hub. The query guard
// activates after the write and matches First's primary-key LIMIT 1 query.
func TestRestoredNativeControlChannelPostCommitReadFailure(t *testing.T) {
	db := restoredControlDB(t, &Channel{}, &Ability{})
	channel := Channel{Id: 31, Name: "before", Key: "fixture-key", Models: "fixture-model"}
	require.NoError(t, db.Create(&channel).Error)
	wrote, hits := false, 0
	failure := errors.New("post-commit channel read unavailable")
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("restored:observe-channel-write", func(tx *gorm.DB) {
		if tx.Error == nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
			wrote = true
		}
	}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("restored:fail-channel-first", func(tx *gorm.DB) {
		limit, limited := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		order, ordered := tx.Statement.Clauses["ORDER BY"].Expression.(clause.OrderBy)
		if wrote && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" && len(tx.Statement.Selects) == 0 && limited && limit.Limit != nil && *limit.Limit == 1 && ordered && len(order.Columns) == 1 && order.Columns[0].Column.Name == clause.PrimaryKey {
			hits++
			tx.AddError(failure)
		}
	}))
	// The baseline obtains its routing defaults from the stored row before the
	// write; no local routing fields or hooks are needed to provoke the failure.
	update := Channel{Id: 31, Name: "after", Models: "fixture-model"}
	err := update.Update()
	require.True(t, wrote, "fixture must perform the channel update")
	require.Equal(t, 1, hits, "fixture must fault exactly the post-write First query")
	if !errors.Is(err, failure) {
		t.Error("CONTROL_CHANNEL_POST_COMMIT_READ: committed channel read error was not returned")
	}
	require.NoError(t, db.Callback().Query().Remove("restored:fail-channel-first"))
	var stored Channel
	require.NoError(t, db.First(&stored, 31).Error)
	require.Equal(t, "after", stored.Name, "failure is after the successful database write")
}
