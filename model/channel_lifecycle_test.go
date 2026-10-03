package model

import (
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Each SQL dialect uses a database created for this test, never an application store.
func TestRetiredChannelDatabaseContracts(t *testing.T) {
	for _, test := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			open := gatewayStartupDatabase(t, test.kind, dsn)
			db, err := open()
			require.NoError(t, err)
			previousDB, previousLog := DB, LOG_DB
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			previousCache := common.MemoryCacheEnabled
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(test.kind, test.kind)
			common.MemoryCacheEnabled = false
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousMainType, previousLogType)
				common.MemoryCacheEnabled = previousCache
				initCol()
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, migrateDB())
			assert.False(t, db.Migrator().HasTable("provider_usage_snapshots"))
			assert.False(t, db.Migrator().HasTable("provider_usage_settings"))
			legacy := Channel{Type: constant.ChannelTypeCodex, Name: "retired-subscription", Key: "legacy-oauth-fixture", Status: common.ChannelStatusEnabled, Models: "gpt-5-codex", Group: "default", ChannelRatio: DefaultChannelRatio, Tag: common.GetPointer("shared")}
			// Seed the historic row exactly as stored by a previous version.
			require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&legacy).Error)
			require.NoError(t, db.Create(&Ability{ChannelId: legacy.Id, Group: "default", Model: legacy.Models, Enabled: true}).Error)
			require.NoError(t, migrateDB())
			restored, err := GetChannelById(legacy.Id, true)
			require.NoError(t, err)
			assert.Equal(t, legacy.Key, restored.Key)
			assert.Equal(t, legacy.Type, restored.Type)
			assert.Equal(t, legacy.Status, restored.Status, "startup must not rewrite historical rows")
			listed, err := GetChannelsByType(0, 10, true, constant.ChannelTypeCodex)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Empty(t, listed[0].Key)
			key, _, relayErr := restored.GetNextEnabledKey()
			require.NotNil(t, relayErr)
			assert.Empty(t, key)
			assert.Contains(t, relayErr.Error(), constant.ErrRetiredChannelType.Error())
			assert.Empty(t, restored.GetEnabledKeyIndexes())
			assert.False(t, ChannelSupportsRequestPath(restored, "/v1/responses", legacy.Models))
			assert.False(t, restored.HasEnabledGatewayKey(legacy.Key))
			assert.Empty(t, GetEnabledModels())
			assert.Empty(t, GetGroupEnabledModels("default"))
			abilities, err := GetAllEnableAbilityWithChannels()
			require.NoError(t, err)
			assert.Empty(t, abilities)
			owners, err := GetPreferredModelOwnerChannelTypes([]string{legacy.Models}, []string{"default"})
			require.NoError(t, err)
			assert.Empty(t, owners)
			assert.ErrorIs(t, restored.AddAbilities(nil), constant.ErrRetiredChannelType)
			assert.ErrorIs(t, restored.UpdateAbilities(nil), constant.ErrRetiredChannelType)
			assert.ErrorIs(t, restored.Save(), constant.ErrRetiredChannelType)
			assert.ErrorIs(t, restored.Update(), constant.ErrRetiredChannelType)
			restored.Type = constant.ChannelTypeOpenAI
			assert.ErrorIs(t, restored.Update(), constant.ErrRetiredChannelType, "changing a retired row into another provider must fail")
			assert.ErrorIs(t, restored.Save(), constant.ErrRetiredChannelType)
			for _, cached := range []bool{false, true} {
				common.MemoryCacheEnabled = cached
				InitChannelCache()
				assert.False(t, UpdateChannelStatus(legacy.Id, "", common.ChannelStatusManuallyDisabled, "test"))
			}
			common.MemoryCacheEnabled = false
			fresh := Channel{Type: constant.ChannelTypeCodex, Key: "new-fixture"}
			assert.ErrorIs(t, fresh.Insert(), constant.ErrRetiredChannelType)
			active := Channel{Type: constant.ChannelTypeOpenAI, Name: "standard-openai", Key: "standard-api-fixture", Status: common.ChannelStatusEnabled, Models: legacy.Models, Group: "default", Tag: legacy.Tag}
			require.NoError(t, active.Insert())
			assert.Equal(t, []string{legacy.Models}, GetEnabledModels())
			for _, mutate := range []func() error{
				func() error { return BatchSetChannelTag([]int{active.Id, legacy.Id}, common.GetPointer("changed")) },
				func() error { return EnableChannelByTag("shared") },
				func() error { return DisableChannelByTag("shared") },
				func() error {
					return EditChannelByTag("shared", common.GetPointer("changed"), nil, nil, nil, nil, nil, nil, nil)
				},
			} {
				assert.ErrorIs(t, mutate(), constant.ErrRetiredChannelType)
			}
			kept, err := GetChannelById(active.Id, true)
			require.NoError(t, err)
			assert.Equal(t, "shared", kept.GetTag())
			assert.Equal(t, common.ChannelStatusEnabled, kept.Status)
			batch := []Channel{{Type: constant.ChannelTypeOpenAI, Key: "batch-first"}, {Type: constant.ChannelTypeCodex, Key: "batch-retired"}}
			assert.ErrorIs(t, BatchInsertChannels(batch), constant.ErrRetiredChannelType)
			var count int64
			require.NoError(t, db.Model(&Channel{}).Where("key = ?", "batch-first").Count(&count).Error)
			assert.Zero(t, count, "mixed creation must roll back")
			require.NoError(t, legacy.Delete())
			_, err = GetChannelById(legacy.Id, false)
			assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
			require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", legacy.Id).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

// seedRetiredLifecycleChannels stores one row per lifecycle case. Hooks are
// skipped because a retired type can only exist as a historical row.
func seedRetiredLifecycleChannels(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db := useOpsTestDB(t, name)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&[]Channel{
		{Id: 1, Name: "enabled", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "k1", Models: "gpt-a", Group: "default"},
		{Id: 2, Name: "manual", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled, Key: "k2", Models: "gpt-a", Group: "default"},
		{Id: 3, Name: "auto", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusAutoDisabled, Key: "k3", Models: "gpt-a", Group: "default"},
		{Id: 57, Name: "retired", Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled, Key: "k57", Models: "gpt-5-codex", Group: "default"},
	}).Error)
	return db
}

func TestChannelServabilityExcludesRetiredTypesInMemoryAndSQL(t *testing.T) {
	db := seedRetiredLifecycleChannels(t, "servable.db")
	var rows []Channel
	require.NoError(t, db.Order("id").Find(&rows).Error)
	inMemory := map[bool][]int{}
	for _, row := range rows {
		inMemory[row.IsServable()] = append(inMemory[row.IsServable()], row.Id)
	}
	var servable, unservable []int
	require.NoError(t, WhereChannelServable(db.Model(&Channel{})).Order("id").Pluck("id", &servable).Error)
	require.NoError(t, WhereChannelUnservable(db.Model(&Channel{})).Order("id").Pluck("id", &unservable).Error)
	assert.Equal(t, []int{1}, servable)
	assert.Equal(t, []int{2, 3, 57}, unservable)
	assert.Equal(t, servable, inMemory[true], "the SQL and in-memory forms must agree")
	assert.Equal(t, unservable, inMemory[false])
	retired := rows[3]
	assert.Equal(t, common.ChannelStatusManuallyDisabled, retired.EffectiveStatus())
	assert.Equal(t, common.ChannelStatusEnabled, retired.Status, "the stored historical status is not rewritten")
}

func TestExcludeChannelTypesAddsNoConditionForEmptyRetiredList(t *testing.T) {
	db := seedRetiredLifecycleChannels(t, "exclude-empty.db")
	var count int64
	require.NoError(t, db.Model(&Channel{}).Where("type NOT IN ?", []int{}).Count(&count).Error)
	assert.Zero(t, count, "an unguarded empty NOT IN matches no rows")
	for _, test := range []struct {
		name     string
		excluded []int
		want     int64
	}{
		{"nil list", nil, 4},
		{"empty list", []int{}, 4},
		{"retired list", []int{constant.ChannelTypeCodex}, 3},
	} {
		require.NoError(t, excludeChannelTypes(db.Model(&Channel{}), "type", test.excluded).Count(&count).Error)
		assert.Equal(t, test.want, count, test.name)
	}
}

func TestDeleteDisabledChannelIncludesRetiredRows(t *testing.T) {
	db := seedRetiredLifecycleChannels(t, "delete-disabled.db")
	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	assert.EqualValues(t, 3, deleted)
	var remaining []int
	require.NoError(t, db.Model(&Channel{}).Order("id").Pluck("id", &remaining).Error)
	assert.Equal(t, []int{1}, remaining, "manual, auto and retired rows are all shown as disabled")
}

func TestFixAbilityKeepsRetiredChannelAbilities(t *testing.T) {
	db := seedRetiredLifecycleChannels(t, "fix-ability.db")
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 57, Group: "default", Model: "gpt-5-codex", Enabled: true},
		{ChannelId: 1, Group: "default", Model: "stale-model", Enabled: true},
		{ChannelId: 999, Group: "default", Model: "orphan", Enabled: true},
	}).Error)
	success, fails, err := FixAbility()
	require.NoError(t, err)
	assert.Equal(t, 3, success, "only the three live channels are repaired")
	assert.Zero(t, fails, "a retired channel is skipped, not a failure")
	var abilities []Ability
	require.NoError(t, db.Order("channel_id").Find(&abilities).Error)
	got := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		got = append(got, fmt.Sprintf("%d:%s:%t", ability.ChannelId, ability.Model, ability.Enabled))
	}
	assert.Equal(t, []string{"1:gpt-a:true", "2:gpt-a:false", "3:gpt-a:false", "57:gpt-5-codex:true"}, got,
		"live rows are rebuilt, the retired row is kept and the deleted channel's row is removed")
}

func TestRetiredChannelCannotResolveTaskCredentials(t *testing.T) {
	channel := &Channel{Id: 57, Type: constant.ChannelTypeCodex, Key: "removed-fixture", Status: common.ChannelStatusEnabled}
	for _, task := range []*Task{
		nil,
		{ChannelId: 57, PrivateData: TaskPrivateData{Key: "legacy-cached-fixture"}},
		{ChannelId: 57, PrivateData: TaskPrivateData{CredentialVersion: 1, CredentialID: "legacy-identity", EndpointID: channel.GatewayEndpointID()}},
	} {
		key, _, err := ResolveTaskCredential(t.Context(), channel, task)
		assert.ErrorIs(t, err, constant.ErrRetiredChannelType)
		assert.Empty(t, key)
		assert.Empty(t, ResolveTaskChannelKey(channel, task))
	}
	key, _, err := ResolveMidjourneyCredential(t.Context(), channel, &Midjourney{ChannelId: 57, PrivateData: TaskPrivateData{CredentialVersion: 1}})
	assert.ErrorIs(t, err, constant.ErrRetiredChannelType)
	assert.Empty(t, key)
}
