package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRouteProfileTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDB := common.MainDatabaseType()
	originalLogDatabase := common.LogDatabaseType()
	originalRedis := common.RedisEnabled
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.UserRouteProfile{}, &model.UserRouteGroup{}, &model.UserRouteEntry{},
		&model.RoutePolicy{}, &model.UserChannelEntitlement{},
	))
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDB, originalLogDatabase)
		common.RedisEnabled = originalRedis
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedRouteProfileFixture(t *testing.T, db *gorm.DB) (int, int, int) {
	t.Helper()
	user := &model.User{Id: 701, Username: "routing-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "routing-token", Name: "routing-token", Status: common.TokenStatusEnabled, Group: "default", ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(token).Error)
	channel := &model.Channel{Id: 7011, Key: "routing-channel-key", Name: "routing-channel", Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-test", ChannelId: channel.Id, Enabled: true}).Error)
	return user.Id, token.Id, channel.Id
}

func TestRouteProfileCreateUpdateAndVersionConflict(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID:  userID,
		TokenID: tokenID,
		Mode:    model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name:     "主线路",
			Enabled:  true,
			Position: 0,
			Entries: []RouteEntryInput{{
				ChannelID: channelID,
				Source:    model.RouteSourcePlatform,
				Enabled:   true,
				Position:  0,
				Weight:    100,
			}},
			Policy: RoutePolicyInput{RetryMode: model.RoutePolicyRetryNextChannel, MaxFailoverAttempts: 1},
		}},
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, int64(1), created.Profile.Version)
	require.Len(t, created.Groups, 1)
	require.Len(t, created.Groups[0].Entries, 1)
	assert.Equal(t, channelID, created.Groups[0].Entries[0].ChannelID)

	updatedInput := RouteProfileInput{
		UserID:  userID,
		TokenID: tokenID,
		Mode:    model.RouteModeManual,
		Version: created.Profile.Version,
		Groups: []RouteGroupInput{{
			ID:       created.Groups[0].Group.ID,
			Name:     "备用线路",
			Enabled:  true,
			Position: 0,
			Entries:  []RouteEntryInput{},
			Policy:   RoutePolicyInput{RetryMode: model.RoutePolicyRetryNone},
		}},
	}
	updated, err := UpdateUserRouteProfile(created.Profile.ID, updatedInput)
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated.Profile.Version)
	assert.Equal(t, "备用线路", updated.Groups[0].Group.Name)
	assert.Empty(t, updated.Groups[0].Entries)

	updatedInput.Version = created.Profile.Version
	_, err = UpdateUserRouteProfile(created.Profile.ID, updatedInput)
	assert.True(t, errors.Is(err, ErrRouteProfileConflict))
}

func TestRouteProfileKeepsMultipleNewGroups(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID:  userID,
		TokenID: tokenID,
		Mode:    model.RouteModeManual,
		Groups: []RouteGroupInput{
			{
				Name: "主线路", Enabled: true, Position: 0,
				Entries: []RouteEntryInput{{
					ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 0,
				}},
			},
			{Name: "备用线路", Enabled: false, Position: 1},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Groups, 2)
	assert.Equal(t, created.Groups[0].Group.ID, *created.Profile.ActiveGroupID)
	assert.Equal(t, "备用线路", created.Groups[1].Group.Name)
}

func TestRouteProfileRejectsDisabledAndForeignChannels(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("status", common.ChannelStatusManuallyDisabled).Error)

	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "disabled", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	assert.True(t, errors.Is(err, ErrRouteProfileValidation))

	_, err = CreateUserRouteProfile(RouteProfileInput{UserID: userID, TokenID: tokenID + 100, Mode: model.RouteModeManual})
	assert.True(t, errors.Is(err, ErrRouteProfileForbidden))
}

func TestValidatePlatformChannelEntitlementHonorsRevocation(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, _, channelID := seedRouteProfileFixture(t, db)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{
		UserID: userID, ChannelID: channelID, Source: model.RouteSourcePlatform,
		Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1,
	}).Error)

	err := ValidatePlatformChannelEntitlement(userID, channelID)
	assert.True(t, errors.Is(err, ErrRouteProfileValidation))
}
