package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func upstreamTestAccount(t *testing.T) *UpstreamAccount {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&UpstreamAccount{}, &UpstreamChannelBinding{}, &ChannelUpstreamRatio{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Migrator().DropTable(&UpstreamChannelBinding{}, &UpstreamAccount{}, &ChannelUpstreamRatio{}))
	})
	a := &UpstreamAccount{Name: "account", Platform: "newapi", BaseURL: "https://example.invalid", Status: "active"}
	require.NoError(t, CreateUpstreamAccount(a))
	return a
}

func TestUpstreamBindingConcurrent1To1(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bindings.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	oldDB, oldType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { DB = oldDB; common.SetMainDatabaseType(oldType); require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Channel{}))

	a := upstreamTestAccount(t)
	concurrentUpstreamBind(t, a)
}

func concurrentUpstreamBind(t *testing.T, a *UpstreamAccount) {
	t.Helper()
	start := make(chan struct{})
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		ch := &Channel{Name: "binding", CreatedTime: 111, Models: "m", Key: "test-key"}
		require.NoError(t, DB.Create(ch).Error)
		b := &UpstreamChannelBinding{AccountID: a.ID, RemoteKeyID: 333, ChannelID: ch.Id, ChannelCreatedTime: 111, ChannelFP: "fingerprint"}
		go func() { <-start; result <- BindUpstreamChannel(b, func(*Channel) error { return nil }) }()
	}
	close(start)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		switch err := <-result; {
		case err == nil:
			success++
		case errors.Is(err, ErrRemoteKeyAlreadyBound):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	other := &UpstreamAccount{Platform: "newapi", Status: "active", Name: "second"}
	require.NoError(t, CreateUpstreamAccount(other))
	shared := Channel{Name: "shared", Models: "m", CreatedTime: 112}
	require.NoError(t, DB.Create(&shared).Error)
	start = make(chan struct{})
	for _, accountID := range []int{a.ID, other.ID} {
		b := UpstreamChannelBinding{AccountID: accountID, ChannelID: shared.Id, RemoteKeyID: 334, ChannelFP: "second-fp", ChannelCreatedTime: 112}
		go func() { <-start; result <- BindUpstreamChannel(&b, func(*Channel) error { return nil }) }()
	}
	close(start)
	left, right := <-result, <-result
	if left != nil {
		left, right = right, left
	}
	require.NoError(t, left)
	require.ErrorIs(t, right, ErrRemoteKeyAlreadyBound)
	require.NoError(t, DB.Where("channel_id = ?", shared.Id).Delete(&UpstreamChannelBinding{}).Error)
}

func TestUpstreamManagementDatabaseMatrix(t *testing.T) {
	useScheduledChannelDatabase(t)
	a := upstreamTestAccount(t)
	require.NoError(t, UpdateUpstreamAccountCAS(a.ID, a.Version, map[string]any{"name": "edited"}))
	require.ErrorIs(t, UpdateUpstreamAccountCAS(a.ID, a.Version, map[string]any{"name": "old"}), ErrUpstreamAccountConflict)
	concurrentUpstreamBind(t, a)
	bindings, err := ListUpstreamBindings(a.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	b := bindings[0]
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", b.ChannelID).Update("created_time", 222).Error)
	changed, err := UpdateChannelRatioFromUpstreamSyncLocked(b.ChannelID, 111, func(*Channel) error { return nil }, "1.4")
	require.False(t, changed)
	require.ErrorIs(t, err, ErrUpstreamRatioConflict)
	b.ID = 0
	b.ChannelCreatedTime = 222
	require.NoError(t, BindUpstreamChannel(&b, func(*Channel) error { return nil }))
	changed, err = UpdateChannelRatioFromUpstreamSyncLocked(b.ChannelID, 222, func(*Channel) error { return nil }, "1.400")
	require.NoError(t, err)
	require.True(t, changed)
	ch, err := GetChannelById(b.ChannelID, true)
	require.NoError(t, err)
	require.Equal(t, "1.4", ch.ChannelRatio)
	require.Equal(t, "test-key", ch.Key)
	t.Run("fingerprint mismatch and failed stale write", func(t *testing.T) {
		b.ChannelFP = UpstreamRatioFingerprint(ch)
		require.NoError(t, DB.Model(&UpstreamChannelBinding{}).Where("id = ?", b.ID).Update("channel_fp", b.ChannelFP).Error)
		require.NoError(t, DB.Model(ch).Update("base_url", "https://changed.invalid").Error)
		injected := errors.New("stale write failed")
		require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test:reject_stale", func(tx *gorm.DB) {
			if _, ok := tx.Statement.Model.(*UpstreamChannelBinding); ok {
				tx.AddError(injected)
			}
		}))
		fire := func() error {
			_, err := UpdateChannelLocked(ch.Id, func(current *Channel) error {
				if err := b.Verify(current); err != nil {
					return err
				}
				current.Status = common.ChannelStatusAutoDisabled
				return nil
			})
			return err
		}
		err := fire()
		require.NoError(t, DB.Callback().Update().Remove("test:reject_stale"))
		require.ErrorIs(t, err, injected)
		stored, e := GetChannelById(ch.Id, true)
		require.NoError(t, e)
		require.Equal(t, ch.Status, stored.Status)
		rows, e := ListUpstreamBindings(a.ID)
		require.NoError(t, e)
		require.False(t, rows[0].Stale)
		require.ErrorIs(t, fire(), ErrUpstreamBindingStale)
		rows, e = ListUpstreamBindings(a.ID)
		require.NoError(t, e)
		require.True(t, rows[0].Stale)
	})
	require.NoError(t, DeleteUpstreamAccount(a.ID))
	bindings, err = ListUpstreamBindings(a.ID)
	require.NoError(t, err)
	require.Empty(t, bindings)
}
