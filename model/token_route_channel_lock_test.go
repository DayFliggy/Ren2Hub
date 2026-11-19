package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// These are local lock-contract diagnostics, not known-upstream exceptions.
// Two channels suffice: batching at 200 rows is not a prerequisite for a cycle.
func TestTokenRouteChannelMutationLockOrderDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("ROUTING_TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("ROUTING_TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("ROUTING_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("ROUTING_TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			for _, operation := range channelMutationOperations {
				t.Run(operation, func(t *testing.T) {
					db := openChannelMutationDatabase(t, driver, dialect)
					runChannelMutationLockRace(t, db, operation)
				})
			}
			for _, operation := range []string{"batch_delete", "batch_set_tag", "disable_tag"} {
				t.Run("over_200/"+operation, func(t *testing.T) {
					runChannelMutationGlobalOrder(t, openChannelMutationDatabase(t, driver, dialect), operation)
				})
			}
			for _, operation := range []string{"delete_disabled", "disable_tag", "enable_tag", "edit_tag"} {
				t.Run("predicate_recheck/"+operation, func(t *testing.T) {
					runChannelMutationPredicateRecheck(t, openChannelMutationDatabase(t, driver, dialect), operation)
				})
			}
			for _, operation := range channelMutationOperations {
				t.Run("ability_rollback/"+operation, func(t *testing.T) {
					runChannelMutationAbilityRollback(t, openChannelMutationDatabase(t, driver, dialect), operation)
				})
			}
			for _, operation := range []string{"disable_tag", "enable_tag", "edit_tag"} {
				for _, outcome := range []string{"no_match", "publication_failure", "bump_failure"} {
					t.Run("sync_topic/"+operation+"/"+outcome, func(t *testing.T) {
						runChannelMutationSyncTopic(t, openChannelMutationDatabase(t, driver, dialect), operation, outcome)
					})
				}
			}
			t.Run("models_rebuild", func(t *testing.T) {
				runChannelMutationModelsRebuild(t, openChannelMutationDatabase(t, driver, dialect))
			})
			for _, stage := range []string{stageEntriesRead, stageEntriesInsert} {
				t.Run("save_orphan_cleanup/"+stage, func(t *testing.T) {
					runChannelSaveOrphanCleanup(t, openChannelMutationDatabase(t, driver, dialect), stage)
				})
			}
		})
	}
}

func openChannelMutationDatabase(t *testing.T, driver gorm.Dialector, dialect string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(driver, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("channel_lock_%d_", time.Now().UnixNano())},
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
	}
	tables := []any{&Token{}, &TokenRouteProfile{}, &TokenRouteEntry{}, &Channel{}, &Ability{}, &SystemTask{}}
	require.NoError(t, db.AutoMigrate(tables...))
	previousDB, redis, cache, previousDialect := DB, common.RedisEnabled, common.MemoryCacheEnabled, common.MainDatabaseType()
	DB, common.RedisEnabled, common.MemoryCacheEnabled = db, false, false
	common.SetMainDatabaseType(common.DatabaseType(dialect))
	t.Cleanup(func() {
		DB, common.RedisEnabled, common.MemoryCacheEnabled = previousDB, redis, cache
		common.SetMainDatabaseType(previousDialect)
		assert.NoError(t, db.Migrator().DropTable(tables...))
		assert.NoError(t, sqlDB.Close())
	})
	return db
}

func runChannelMutationLockRace(t *testing.T, db *gorm.DB, operation string) {
	t.Helper()
	const lowID, highID = 960001, 960002
	status := common.ChannelStatusEnabled
	if operation == "delete_disabled" || operation == "enable_tag" {
		status = common.ChannelStatusManuallyDisabled
	}
	tag := "route-lock-diagnostic"
	for _, id := range []int{highID, lowID} {
		require.NoError(t, db.Create(&Channel{
			Id: id, Key: "fixture", Status: status, Models: "route-model", Tag: &tag,
		}).Error)
		require.NoError(t, db.Create(&Ability{
			ChannelId: id, Model: "route-model", Enabled: status == common.ChannelStatusEnabled, Tag: &tag,
		}).Error)
	}
	fixture := raceFixture{db: db, token: Token{UserId: 901, Key: "channel-lock-token"}, ids: map[string]int{"A": lowID, "B": highID}}
	require.NoError(t, fixture.token.InsertWithRouting(TokenRouteManual))
	require.NoError(t, fixture.save(fixture.route(1, "A", "B")))
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		var physicalOrder []int
		require.NoError(t, db.Model(&Channel{}).Order("ctid").Pluck("id", &physicalOrder).Error)
		require.Equal(t, []int{highID, lowID}, physicalOrder, "reverse heap layout is a required diagnostic premise")
	}

	beforeRevision := channelsRevision(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	DB = db.WithContext(ctx)
	holder := db.WithContext(ctx).Begin()
	require.NoError(t, holder.Error)
	defer holder.Rollback()
	var held []int
	require.NoError(t, lockForUpdate(holder.Model(&Channel{})).Where("id = ?", lowID).Pluck("id", &held).Error)
	require.Equal(t, []int{lowID}, held)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	baseline := sqlDB.Stats().WaitCount
	saveDone, mutationDone := make(chan struct{}), make(chan struct{})
	var saveErr, mutationErr error
	mutationStarted := false
	defer func() {
		// Release before joining, including on failed fixture preconditions.
		_ = holder.Rollback().Error
		cancel()
		<-saveDone
		if mutationStarted {
			<-mutationDone
		}
	}()
	go func() {
		defer close(saveDone)
		_, saveErr = SaveTokenRouting(ctx, fixture.token.UserId, fixture.token.Id, fixture.route(2, "B", "A"), fixture.allowed())
	}()
	waitLockWaiters(t, db, sqlDB, baseline, 1)
	mutationStarted = true
	go func() {
		defer close(mutationDone)
		mutationErr = mutateChannelLockFixture(operation, tag, []int{highID, lowID})
	}()
	waitLockWaiters(t, db, sqlDB, baseline, 2)
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		require.False(t, channelRowLockedNow(t, db, highID), "both operations must wait for the lower ID before locking the higher ID")
	}
	require.NoError(t, holder.Commit().Error)
	<-saveDone
	<-mutationDone
	DB = db
	t.Logf("local_lock_diagnostic operation=%s rows=2 dialect=%s save_err=%v mutation_err=%v", operation, common.MainDatabaseType(), saveErr, mutationErr)
	if mutationErr == nil && (operation == "disable_tag" || operation == "enable_tag" || operation == "edit_tag") {
		assert.Greater(t, channelsRevision(t), beforeRevision, "committed tag mutations must publish the native channels sync topic")
	}

	// Check transaction effects before rejecting deadlocks, so failures retain
	// evidence of rollback and of route/ability integrity.
	view, err := ReadTokenRouting(context.Background(), fixture.token.Id)
	require.NoError(t, err)
	wantVersion := int64(3)
	if saveErr != nil {
		wantVersion = 2
	}
	assert.Equal(t, wantVersion, view.Version, "a failed save must not publish its revision")
	var channels []Channel
	var abilities []Ability
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.NoError(t, db.Order("channel_id").Find(&abilities).Error)
	deleted := mutationErr == nil && (operation == "batch_delete" || operation == "delete_disabled")
	if deleted {
		assert.Empty(t, channels)
		assert.Empty(t, abilities)
		assert.Empty(t, view.Entries)
	} else {
		require.Len(t, channels, 2)
		require.Len(t, abilities, 2)
		wantStatus, wantPriority := status, int64(0)
		if mutationErr == nil {
			switch operation {
			case "disable_tag":
				wantStatus = common.ChannelStatusManuallyDisabled
			case "enable_tag":
				wantStatus = common.ChannelStatusEnabled
			case "edit_tag":
				wantPriority = 7
			}
		}
		for i, channel := range channels {
			assert.Equal(t, wantStatus, channel.Status)
			assert.Equal(t, wantPriority, channel.GetPriority())
			assert.Equal(t, wantStatus == common.ChannelStatusEnabled, abilities[i].Enabled)
			assert.NotNil(t, abilities[i].Priority)
			if abilities[i].Priority != nil {
				assert.Equal(t, wantPriority, *abilities[i].Priority)
			}
			wantTag := tag
			if mutationErr == nil && operation == "batch_set_tag" {
				wantTag = "retagged"
			}
			assert.Equal(t, &wantTag, channel.Tag)
			assert.Equal(t, channel.Tag, abilities[i].Tag)
		}
		wantOrder := []string{"B", "A"}
		if saveErr != nil {
			wantOrder = []string{"A", "B"}
		}
		assert.Equal(t, wantOrder, fixture.entryLabels(t))
	}
	requireNoOrphanRoutes(t, db)
	assert.NoError(t, mutationErr, "local channel mutation failure is never known-upstream")
	if deleted && errors.Is(saveErr, ErrTokenRouteForbidden) {
		return // A completed deletion legitimately invalidates the saved input.
	}
	assert.NoError(t, saveErr, "local save deadlocks must fail the contract")
}

func channelRowLockedNow(t *testing.T, db *gorm.DB, id int) bool {
	t.Helper()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	var ids []int
	err := tx.Model(&Channel{}).Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).Where("id = ?", id).Pluck("id", &ids).Error
	if err == nil {
		return false
	}
	var state interface{ SQLState() string }
	require.ErrorAs(t, err, &state)
	require.Equal(t, "55P03", state.SQLState(), "only lock_not_available proves another operation owns this row")
	return true
}

func mutateChannelLockFixture(operation, tag string, ids []int) error {
	switch operation {
	case "batch_delete":
		count, err := BatchDeleteChannels(ids)
		if err != nil {
			return err
		}
		if count != 2 {
			return fmt.Errorf("batch delete affected %d channels, want 2", count)
		}
		return DeleteOrphanChannelRouteEntries(context.Background())
	case "delete_disabled":
		deleted, err := DeleteDisabledChannelIDs()
		if err != nil {
			return err
		}
		if len(deleted) != 2 {
			return fmt.Errorf("disabled deletion affected %d channels, want 2", len(deleted))
		}
		return DeleteOrphanChannelRouteEntries(context.Background())
	case "disable_tag":
		return DisableChannelByTag(tag)
	case "enable_tag":
		return EnableChannelByTag(tag)
	case "edit_tag":
		priority := int64(7)
		return EditChannelByTag(tag, nil, nil, nil, &priority, nil, nil, nil)
	case "batch_set_tag":
		newTag := "retagged"
		return BatchSetChannelTag(ids, &newTag)
	default:
		return fmt.Errorf("unknown channel mutation %q", operation)
	}
}
