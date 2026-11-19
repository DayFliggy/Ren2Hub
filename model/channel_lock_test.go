package model

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var channelMutationOperations = []string{"batch_delete", "delete_disabled", "disable_tag", "enable_tag", "edit_tag", "batch_set_tag"}

func seedChannelMutationRows(t *testing.T, db *gorm.DB, status int, tag string) {
	t.Helper()
	for _, id := range []int{2, 1} {
		channel := Channel{Id: id, Key: "fixture", Status: status, Models: "old-model", Tag: &tag}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, channel.AddAbilities(db))
	}
}

func recordChannelPublications(t *testing.T, verify func(int) error) *[]int {
	t.Helper()
	previous := ChannelRoutingChanged
	t.Cleanup(func() { ChannelRoutingChanged = previous })
	ids := []int{}
	ChannelRoutingChanged = func(id int) error {
		ids = append(ids, id)
		if verify != nil {
			return verify(id)
		}
		return nil
	}
	return &ids
}

func runChannelMutationGlobalOrder(t *testing.T, db *gorm.DB, operation string) {
	const count = 205
	tag := "selected"
	ids := make([]int, 0, count+2)
	fixtureChannels := make([]Channel, 0, count)
	for id := count; id > 0; id-- {
		fixtureChannels = append(fixtureChannels, Channel{Id: id, Key: "fixture", Status: common.ChannelStatusEnabled, Models: "old-model", Tag: &tag})
		ids = append(ids, id)
	}
	// Keep the reverse insertion order without one transaction per fixture row.
	require.NoError(t, db.CreateInBatches(&fixtureChannels, 200).Error)
	fixtureAbilities := make([]Ability, 0, count)
	for _, channel := range fixtureChannels {
		fixtureAbilities = append(fixtureAbilities, Ability{
			ChannelId: channel.Id, Model: channel.Models, Enabled: channel.Status == common.ChannelStatusEnabled,
			Priority: channel.Priority, Weight: uint(channel.GetWeight()), Tag: channel.Tag,
		})
	}
	require.NoError(t, db.CreateInBatches(&fixtureAbilities, 200).Error)
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		var physicalOrder []int
		require.NoError(t, db.Model(&Channel{}).Order("ctid").Pluck("id", &physicalOrder).Error)
		require.Equal(t, ids, physicalOrder, "batched fixture setup must preserve the reverse heap layout")
	}
	ids = append(ids, count, count+99) // Duplicate and missing IDs do not widen the write.
	input := slices.Clone(ids)
	published := recordChannelPublications(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	DB = db.WithContext(ctx)
	holder := db.WithContext(ctx).Begin()
	require.NoError(t, holder.Error)
	defer holder.Rollback()
	var held Channel
	require.NoError(t, lockForUpdate(holder).First(&held, 1).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	baseline := sqlDB.Stats().WaitCount
	done := make(chan struct{})
	var mutationErr error
	var deleted int64
	defer func() {
		_ = holder.Rollback().Error
		cancel()
		<-done
	}()
	go func() {
		defer close(done)
		switch operation {
		case "batch_delete":
			deleted, mutationErr = BatchDeleteChannels(ids)
		case "disable_tag":
			mutationErr = DisableChannelByTag(tag)
		case "batch_set_tag":
			tag := "new-tag"
			mutationErr = BatchSetChannelTag(ids, &tag)
		default:
			mutationErr = fmt.Errorf("unexpected operation %s", operation)
		}
	}()
	waitLockWaiters(t, db, sqlDB, baseline, 1)
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		require.False(t, channelRowLockedNow(t, db, count), "a later chunk must not lock high IDs before the first ID")
	}
	require.NoError(t, holder.Commit().Error)
	<-done
	DB = db
	require.NoError(t, mutationErr)
	require.Equal(t, input, ids, "locking must not reorder the caller's slice")
	var channels []Channel
	var abilities []Ability
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.NoError(t, db.Order("channel_id").Find(&abilities).Error)
	if operation == "batch_delete" {
		require.EqualValues(t, count, deleted)
		require.Empty(t, channels)
		require.Empty(t, abilities)
		require.Empty(t, *published)
	} else {
		require.Len(t, channels, count)
		require.Len(t, abilities, count)
		require.Len(t, *published, count)
		for i := range channels {
			require.Equal(t, channels[i].Id, (*published)[i])
			if operation == "disable_tag" {
				require.Equal(t, common.ChannelStatusManuallyDisabled, channels[i].Status)
				require.False(t, abilities[i].Enabled)
				require.Equal(t, tag, channels[i].GetTag())
			} else {
				require.Equal(t, "new-tag", channels[i].GetTag())
			}
			require.Equal(t, channels[i].Tag, abilities[i].Tag)
		}
	}
}

func runChannelMutationPredicateRecheck(t *testing.T, db *gorm.DB, operation string) {
	status := common.ChannelStatusEnabled
	if operation == "delete_disabled" || operation == "enable_tag" {
		status = common.ChannelStatusManuallyDisabled
	}
	seedChannelMutationRows(t, db, status, "selected")
	other := "other"
	third := Channel{Id: 3, Key: "fixture", Status: common.ChannelStatusEnabled, Models: "old-model", Tag: &other}
	require.NoError(t, db.Create(&third).Error)
	require.NoError(t, third.AddAbilities(db))
	published := recordChannelPublications(t, nil)
	fired := false
	// Change the predicate after the candidate read. SQLite's single connection
	// uses the same tx; external engines commit through another connection, so
	// the locking read must see a version newer than the candidate snapshot.
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:predicate_changed", func(tx *gorm.DB) {
		if fired || tx.Error != nil || tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Channel" {
			return
		}
		if _, locked := tx.Statement.Clauses["FOR"]; locked {
			return
		}
		if _, candidateIDs := tx.Statement.Dest.(*[]int); !candidateIDs {
			return
		}
		fired = true
		change := func(writer *gorm.DB) error {
			if operation == "delete_disabled" {
				for id, next := range map[int]int{1: common.ChannelStatusEnabled, 3: common.ChannelStatusManuallyDisabled} {
					if err := writer.Model(&Channel{}).Where("id = ?", id).Update("status", next).Error; err != nil {
						return err
					}
					if err := updateAbilityStatus(writer.Where("channel_id = ?", id), next == common.ChannelStatusEnabled); err != nil {
						return err
					}
				}
				return writer.Model(&Channel{}).Where("id = ?", 2).Update("key", "rotated-credential-fixture").Error
			}
			for id, tag := range map[int]string{1: "other", 3: "selected"} {
				if err := writer.Model(&Channel{}).Where("id = ?", id).Update("tag", tag).Error; err != nil {
					return err
				}
				if err := updateAbilityFields(writer.Where("channel_id = ?", id), &tag, nil, nil); err != nil {
					return err
				}
			}
			return nil
		}
		if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			tx.AddError(change(tx.Session(&gorm.Session{NewDB: true})))
		} else {
			tx.AddError(db.Transaction(change))
		}
	}))
	if operation == "delete_disabled" {
		deleted, err := DeleteDisabledChannels()
		require.NoError(t, err)
		require.Len(t, deleted, 1, "re-enabled and newly disabled rows were not in the locked predicate set")
		require.Equal(t, 2, deleted[0].Id)
		require.Equal(t, "rotated-credential-fixture", deleted[0].Key, "revoke must use the credential from the locked deletion, not an earlier snapshot")
	} else {
		require.NoError(t, mutateChannelLockFixture(operation, "selected", []int{2, 1}))
		require.Equal(t, []int{2}, *published)
	}
	require.True(t, fired)
	var channels []Channel
	var abilities []Ability
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.NoError(t, db.Order("channel_id").Find(&abilities).Error)
	require.Equal(t, len(channels), len(abilities))
	for index, channel := range channels {
		if channel.Id != 2 {
			require.Zero(t, channel.GetPriority(), "new predicate members must not be edited")
			if operation != "delete_disabled" {
				want := status
				if channel.Id == 3 {
					want = common.ChannelStatusEnabled
				}
				require.Equal(t, want, channel.Status)
			}
		}
		require.Equal(t, channel.Tag, abilities[index].Tag)
		require.Equal(t, channel.Status == common.ChannelStatusEnabled, abilities[index].Enabled)
	}
}

func runChannelMutationAbilityRollback(t *testing.T, db *gorm.DB, operation string) {
	status := common.ChannelStatusEnabled
	if operation == "delete_disabled" || operation == "enable_tag" {
		status = common.ChannelStatusManuallyDisabled
	}
	seedChannelMutationRows(t, db, status, "selected")
	var beforeChannels []Channel
	var beforeAbilities []Ability
	require.NoError(t, db.Order("id").Find(&beforeChannels).Error)
	require.NoError(t, db.Order("channel_id").Find(&beforeAbilities).Error)
	published := recordChannelPublications(t, nil)
	beforeRevision := channelsRevision(t)
	injected := errors.New("ability write failure fixture")
	fail := func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(injected)
		}
	}
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:ability_delete_failure", fail))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:ability_update_failure", fail))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:ability_create_failure", fail))
	require.ErrorIs(t, mutateChannelLockFixture(operation, "selected", []int{2, 1}), injected)
	var channels []Channel
	var abilities []Ability
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.NoError(t, db.Order("channel_id").Find(&abilities).Error)
	require.Equal(t, beforeChannels, channels, "channel writes must roll back with ability writes")
	require.Equal(t, beforeAbilities, abilities)
	require.Empty(t, *published, "failed transactions must not publish")
	require.Equal(t, beforeRevision, channelsRevision(t), "rolled-back mutations must not publish a sync task")
}

func runChannelMutationModelsRebuild(t *testing.T, db *gorm.DB) {
	seedChannelMutationRows(t, db, common.ChannelStatusEnabled, "selected")
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 2).Update("status", common.ChannelStatusManuallyDisabled).Error)
	merged := "merged"
	other := Channel{Id: 3, Key: "untouched", Status: common.ChannelStatusEnabled, Models: "other-model", Tag: &merged}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, other.AddAbilities(db))
	require.NoError(t, updateAbilityStatus(db.Where("channel_id = ?", 3), false))
	// Whitespace normalization is separately preserved as a restored-native diagnostic.
	models, priority, weight := "beta,alpha,beta", int64(0), uint(0)
	published := recordChannelPublications(t, func(id int) error {
		// This read uses global DB deliberately: publication must be after commit,
		// also on SQLite's single connection, and must see both durable tables.
		var channel Channel
		if err := db.First(&channel, id).Error; err != nil {
			return err
		}
		var abilities []Ability
		if err := db.Where("channel_id = ?", id).Order("model").Find(&abilities).Error; err != nil {
			return err
		}
		if channel.GetTag() != merged || len(abilities) != 2 || abilities[0].Model != "alpha" || abilities[1].Model != "beta" {
			return fmt.Errorf("publication saw stale channel or ability data")
		}
		return nil
	})
	require.NoError(t, EditChannelByTag("selected", &merged, nil, &models, &priority, &weight, nil, nil))
	require.Equal(t, []int{1, 2}, *published)
	var abilities []Ability
	require.NoError(t, db.Order("channel_id, model").Find(&abilities).Error)
	require.Len(t, abilities, 5)
	for _, ability := range abilities {
		require.Equal(t, &merged, ability.Tag)
		if ability.ChannelId == 3 {
			require.Equal(t, "other-model", ability.Model)
			require.False(t, ability.Enabled, "new-tag collision must not rebuild an unrelated channel")
		} else {
			require.Contains(t, []string{"alpha", "beta"}, ability.Model)
			require.Equal(t, ability.ChannelId == 1, ability.Enabled)
			require.Equal(t, &priority, ability.Priority)
			require.Zero(t, ability.Weight)
		}
	}
	// Metadata-only updates preserve per-model enabled flags and write zero weight.
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 1).Update("weight", 9).Error)
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 1).Updates(map[string]any{"weight": 9, "enabled": false}).Error)
	ChannelRoutingChanged = nil
	require.NoError(t, EditChannelByTag("merged", nil, nil, nil, nil, &weight, nil, nil))
	require.NoError(t, db.Where("channel_id = ?", 1).Find(&abilities).Error)
	for _, ability := range abilities {
		require.Zero(t, ability.Weight)
		require.False(t, ability.Enabled)
	}
}

func runChannelSaveOrphanCleanup(t *testing.T, db *gorm.DB, stage string) {
	fixture := seedRaceToken(t, db, 0, []string{"A", "B"})
	require.NoError(t, db.Create(&TokenRouteEntry{TokenID: fixture.token.Id, ChannelID: 999999, Position: 2, Enabled: true}).Error)
	barrier := installRouteBarrier(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	DB = db.WithContext(ctx)
	_, saveErr, cleanupErr := barrier.race(t, db, stage,
		func() error { return fixture.save(fixture.route(2, "B", "A")) },
		func() error { return DeleteOrphanChannelRouteEntries(ctx) })
	DB = db
	require.NoError(t, saveErr)
	require.NoError(t, cleanupErr)
	assert.Equal(t, []string{"B", "A"}, fixture.entryLabels(t))
	requireNoOrphanRoutes(t, db)
}

// Exercise the public tag entrypoints through the same database fixture as the
// lock matrix. Publication errors remain errors after the durable commit.
func runChannelMutationSyncTopic(t *testing.T, db *gorm.DB, operation, outcome string) {
	t.Helper()
	status := common.ChannelStatusEnabled
	if operation == "enable_tag" {
		status = common.ChannelStatusManuallyDisabled
	}
	if outcome != "no_match" {
		seedChannelMutationRows(t, db, status, "selected")
	}
	beforeRevision := channelsRevision(t)
	injected := errors.New("tag sync publication fixture")
	published := recordChannelPublications(t, func(id int) error {
		if outcome != "publication_failure" {
			return nil
		}
		// A global-DB read here also proves the mutation has committed.
		var channel Channel
		require.NoError(t, db.First(&channel, id).Error)
		require.Greater(t, channelsRevision(t), beforeRevision, "sync task must precede fallible local publication")
		return injected
	})
	bumpAttempts := 0
	if outcome == "bump_failure" {
		const callback = "test:fail_tag_sync_task"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "SystemTask" {
				bumpAttempts++
				tx.AddError(injected)
			}
		}))
		t.Cleanup(func() { assert.NoError(t, db.Callback().Create().Remove(callback)) })
	}
	err := mutateChannelLockFixture(operation, "selected", nil)
	if outcome == "publication_failure" {
		require.ErrorIs(t, err, injected, "retain the existing post-commit publication error")
		require.Equal(t, []int{1}, *published, "publication still stops at its first error")
	} else {
		require.NoError(t, err)
	}
	if outcome == "bump_failure" {
		require.Equal(t, 1, bumpAttempts)
		require.Equal(t, beforeRevision, channelsRevision(t))
		require.Equal(t, []int{1, 2}, *published, "failed best-effort bump does not skip routing publication")
	} else {
		require.Greater(t, channelsRevision(t), beforeRevision, "successful empty or nonempty tag write must bump the native topic")
		var tasks []SystemTask
		require.NoError(t, db.Where("type = ? AND id > ?", SyncTaskType(SyncTopicChannels), beforeRevision).Find(&tasks).Error)
		require.Len(t, tasks, 1)
		require.Equal(t, SystemTaskStatusPending, tasks[0].Status)
	}
	var channels []Channel
	var abilities []Ability
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.NoError(t, db.Order("channel_id").Find(&abilities).Error)
	if outcome == "no_match" {
		require.Empty(t, channels)
		require.Empty(t, abilities)
		require.Empty(t, *published)
		return
	}
	require.Len(t, channels, 2)
	require.Len(t, abilities, 2)
	for i, channel := range channels {
		switch operation {
		case "disable_tag":
			require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
			require.False(t, abilities[i].Enabled)
		case "enable_tag":
			require.Equal(t, common.ChannelStatusEnabled, channel.Status)
			require.True(t, abilities[i].Enabled)
		case "edit_tag":
			require.EqualValues(t, 7, channel.GetPriority())
			require.EqualValues(t, 7, *abilities[i].Priority)
		}
	}
}
