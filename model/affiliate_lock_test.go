package model

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The participant lock takes soft-deleted rows so the lock order never depends
// on deletion, but the user itself must still be a live account: soft-deleted
// before the transaction or while it waited for the lock, it is not found. A
// soft-deleted inviter does not stop its invitee's operations.
func TestLockAffiliateParticipantsRejectsASoftDeletedUser(t *testing.T) {
	lock := func(userID int) (*User, error) {
		var locked *User
		err := DB.Transaction(func(tx *gorm.DB) error {
			var err error
			locked, err = lockAffiliateParticipantsTx(tx, userID)
			return err
		})
		return locked, err
	}

	t.Run("deleted before", func(t *testing.T) {
		db, _, invitee := affiliateTestDB(t)
		require.NoError(t, db.Delete(&User{}, invitee.Id).Error)
		_, err := lock(invitee.Id)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("deleted while waiting for the lock", func(t *testing.T) {
		db, _, invitee := affiliateTestDB(t)
		// Soft-delete the user right after the unlocked reference read, inside
		// the same transaction, as a concurrent deletion committed before the
		// lock was granted would appear.
		const name = "test:soft-delete-after-reference-read"
		fired := false
		require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(d *gorm.DB) {
			if fired || d.Statement.Table != "users" || !slices.Contains(d.Statement.Selects, "inviter_id") {
				return
			}
			fired = true
			if err := d.Session(&gorm.Session{NewDB: true}).Model(&User{}).Where("id = ?", invitee.Id).Update("deleted_at", time.Now()).Error; err != nil {
				_ = d.AddError(err)
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
		_, err := lock(invitee.Id)
		require.True(t, fired)
		assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "got %v", err)
	})

	t.Run("soft-deleted inviter", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		require.NoError(t, db.Delete(&User{}, inviter.Id).Error)
		locked, err := lock(invitee.Id)
		require.NoError(t, err)
		assert.Equal(t, invitee.Id, locked.Id)
	})
}
