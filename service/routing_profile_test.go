package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
		&model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{},
		&model.ChannelHealth{},
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

func seedRoutePreviewChannel(t *testing.T, db *gorm.DB, channelID int) int {
	t.Helper()
	channel := &model.Channel{
		Id: channelID, Key: fmt.Sprintf("routing-preview-key-%d", channelID), Name: fmt.Sprintf("routing-preview-%d", channelID),
		Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-test", ChannelId: channelID, Enabled: true}).Error)
	return channelID
}

func publishRoutePreviewCapability(t *testing.T, channelID int, endpointTypes []string, state string) {
	t.Helper()
	groups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpoints, err := common.Marshal(endpointTypes)
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, fmt.Sprintf("preview-source-%d", channelID), "preview-catalog", []model.ChannelModelCapability{{
		RequestModel: "gpt-test", ActualModel: "gpt-test", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: state,
	}}))
}

func findRoutePreviewEntry(t *testing.T, preview *RouteProfilePreview, channelID int) RoutePreviewEntry {
	t.Helper()
	for _, entry := range preview.Entries {
		if entry.ChannelID == channelID {
			return entry
		}
	}
	t.Fatalf("preview entry for channel %d not found", channelID)
	return RoutePreviewEntry{}
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

func TestRouteProfileRejectsDuplicateChannelAcrossGroups(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)

	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{
			{
				Name: "primary", Enabled: true, Position: 0,
				Entries: []RouteEntryInput{{
					ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
				}},
			},
			{
				Name: "saved preset", Enabled: true, Position: 1,
				Entries: []RouteEntryInput{{
					ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
				}},
			},
		},
	})

	assert.ErrorIs(t, err, ErrRouteProfileValidation)
}

func TestRouteProfilePreviewUsesActiveGroupOrderAndSamePositionWeight(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, firstChannelID := seedRouteProfileFixture(t, db)
	secondChannelID := seedRoutePreviewChannel(t, db, firstChannelID+1)
	publishRoutePreviewCapability(t, firstChannelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	publishRoutePreviewCapability(t, secondChannelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{
			{
				Name: "主线路", Enabled: true, Position: 0,
				Entries: []RouteEntryInput{
					{ChannelID: firstChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 0, Weight: 20},
					{ChannelID: secondChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 0, Weight: 80},
				},
				Policy: RoutePolicyInput{LoadBalance: true, RetryMode: model.RoutePolicyRetryNextChannel},
			},
			{Name: "备用线路", Enabled: true, Position: 1},
		},
	})
	require.NoError(t, err)
	require.Len(t, created.Groups, 2)
	require.Len(t, created.Groups[0].Entries, 2)
	assert.Equal(t, secondChannelID, created.Groups[0].Entries[0].ChannelID)
	assert.Equal(t, firstChannelID, created.Groups[0].Entries[1].ChannelID)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	require.NotNil(t, preview.ActiveGroup)
	assert.Equal(t, created.Groups[0].Group.ID, preview.ActiveGroup.ID)
	assert.Equal(t, RoutePreviewSelectionWeighted, preview.SelectionMode)
	assert.Equal(t, secondChannelID, preview.PreferredChannelID)
	assert.Equal(t, []int{secondChannelID, firstChannelID}, preview.CandidateChannelIDs)
	assert.Empty(t, preview.FilterReasonCounts)
	assert.False(t, preview.LiveSelection)
	assert.True(t, preview.RuntimeRecheckRequired)
	assert.Equal(t, []string{"price_qualification", "quota_qualification", "security_policy"}, preview.RuntimeRecheckReasons)
}

func TestRouteProfilePreviewKeepsResolvableMixedCapabilityEligible(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Model(&model.ChannelModelCapability{}).
		Where("channel_id = ?", channelID).Update("is_mixed", true).Error)

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "混合模型", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
	assert.True(t, preview.HasMixed)
	assert.Zero(t, preview.FilterReasonCounts[ShadowFilterUnknownCapability])
	assert.Empty(t, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewIncludesChannelModelHealthSummary(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: "", State: model.RouteHealthStateOpen,
		FailureCount: 3, CooldownUntil: 1_800_000_000, HealthEpoch: 4,
		LastLatencyMS: 120, FirstTokenLatencyMS: 45, UpdatedAt: 1_700_000_100,
	}).Error)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "健康摘要", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	entry := findRoutePreviewEntry(t, preview, channelID)
	assert.Equal(t, RoutePreviewHealthSummary{
		State: model.RouteHealthStateOpen, FailureCount: 3, CooldownUntil: 1_800_000_000,
		HealthEpoch: 4, LastLatencyMS: 120, FirstTokenLatencyMS: 45, UpdatedAt: 1_700_000_100,
	}, entry.Health)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, RouteCandidateFilterHealthUnavailable, entry.FilterReason)

	require.NoError(t, db.Model(&model.ChannelHealth{}).
		Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, "gpt-test", "").
		Updates(map[string]any{"state": model.RouteHealthStateClosed, "cooldown_until": 0}).Error)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: RouteKeyScope("routing-channel-key"),
		State: model.RouteHealthStateOpen, FailureCount: 1, CooldownUntil: common.GetTimestamp() + 60,
	}).Error)
	preview, err = PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	entry = findRoutePreviewEntry(t, preview, channelID)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, RouteFilterKeyUnavailable, entry.FilterReason)
}

func TestRouteProfilePreviewFiltersDisabledTokenAndUnavailableHealth(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "资格", Enabled: true, Entries: []RouteEntryInput{{
			ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
		}}}},
	})
	require.NoError(t, err)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("status", common.TokenStatusDisabled).Error)
	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, ShadowFilterTokenForbidden, findRoutePreviewEntry(t, preview, channelID).FilterReason)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("status", common.TokenStatusEnabled).Error)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: "", State: model.RouteHealthStateOpen,
		FailureCount: 3, CooldownUntil: time.Now().Add(time.Minute).Unix(), HealthEpoch: 2,
	}).Error)
	preview, err = PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteCandidateFilterHealthUnavailable, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewFiltersChannelWhenEveryKeyIsUnavailable(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	channel := model.Channel{}
	require.NoError(t, db.First(&channel, "id = ?", channelID).Error)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: RouteKeyScope(channel.Key), State: model.RouteHealthStateOpen,
		FailureCount: 1, CooldownUntil: time.Now().Add(time.Minute).Unix(), HealthEpoch: 2,
	}).Error)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "Key 健康", Enabled: true, Entries: []RouteEntryInput{{
			ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
		}}}},
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, RouteFilterKeyUnavailable, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewFiltersUnresolvedConflictingAndUnsupportedCapabilities(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, eligibleChannelID := seedRouteProfileFixture(t, db)
	conflictChannelID := seedRoutePreviewChannel(t, db, eligibleChannelID+1)
	unresolvedChannelID := seedRoutePreviewChannel(t, db, eligibleChannelID+2)
	unsupportedChannelID := seedRoutePreviewChannel(t, db, eligibleChannelID+3)
	publishRoutePreviewCapability(t, eligibleChannelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	publishRoutePreviewCapability(t, conflictChannelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateConflict)
	publishRoutePreviewCapability(t, unresolvedChannelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateUnresolved)
	publishRoutePreviewCapability(t, unsupportedChannelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateUnsupported)

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "能力过滤", Enabled: true,
			Entries: []RouteEntryInput{
				{ChannelID: eligibleChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 0},
				{ChannelID: conflictChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 1},
				{ChannelID: unresolvedChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 2},
				{ChannelID: unsupportedChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 3},
			},
		}},
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, []int{eligibleChannelID}, preview.CandidateChannelIDs)
	assert.Equal(t, ShadowFilterMappingConflict, findRoutePreviewEntry(t, preview, conflictChannelID).FilterReason)
	assert.Equal(t, ShadowFilterUnknownCapability, findRoutePreviewEntry(t, preview, unresolvedChannelID).FilterReason)
	assert.Equal(t, ShadowFilterUnsupported, findRoutePreviewEntry(t, preview, unsupportedChannelID).FilterReason)
}

func TestRouteProfilePreviewUsesOnlyActiveCapabilitySnapshot(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	firstFence, err := model.GetChannelCapabilitySnapshotFence(context.Background(), channelID)
	require.NoError(t, err)
	groupsJSON, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpointsJSON, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, firstFence, "preview-source-new", "preview-catalog-new", []model.ChannelModelCapability{{
		RequestModel: "gpt-test", ActualModel: "gpt-test-new", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groupsJSON), EndpointTypes: string(endpointsJSON), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "当前快照", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	entry := findRoutePreviewEntry(t, preview, channelID)
	assert.Equal(t, int64(2), entry.SnapshotVersion)
	assert.Equal(t, "preview-catalog-new", entry.CatalogVersion)
	assert.Equal(t, "gpt-test-new", entry.ActualModel)
	assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
}

func TestRouteProfilePreviewKeepsActiveSnapshotAfterRefreshFailure(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	fence, err := model.GetChannelCapabilitySnapshotFence(context.Background(), channelID)
	require.NoError(t, err)
	require.NoError(t, model.MarkChannelCapabilityRefreshFailure(channelID, fence, "failed-source", "failed-catalog"))

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "刷新失败保留旧快照", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
	assert.Empty(t, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewPreservesUnavailableEntriesWithReasons(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, availableChannelID := seedRouteProfileFixture(t, db)
	disabledChannelID := seedRoutePreviewChannel(t, db, availableChannelID+1)
	revokedChannelID := seedRoutePreviewChannel(t, db, availableChannelID+2)
	abilityDisabledChannelID := seedRoutePreviewChannel(t, db, availableChannelID+3)
	missingSnapshotChannelID := seedRoutePreviewChannel(t, db, availableChannelID+4)
	for _, channelID := range []int{availableChannelID, disabledChannelID, revokedChannelID, abilityDisabledChannelID} {
		publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	}

	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "主线路", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{
				{ChannelID: availableChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 0},
				{ChannelID: disabledChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 1},
				{ChannelID: revokedChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 2},
				{ChannelID: abilityDisabledChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 3},
				{ChannelID: missingSnapshotChannelID, Source: model.RouteSourcePlatform, Enabled: true, Position: 4},
			},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", disabledChannelID).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{
		UserID: userID, ChannelID: revokedChannelID, Source: model.RouteSourcePlatform,
		Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1,
	}).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", abilityDisabledChannelID).Update("enabled", false).Error)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, []int{availableChannelID}, preview.CandidateChannelIDs)
	assert.Equal(t, ShadowFilterChannelDisabled, findRoutePreviewEntry(t, preview, disabledChannelID).FilterReason)
	assert.Equal(t, ShadowFilterEntitlementRevoked, findRoutePreviewEntry(t, preview, revokedChannelID).FilterReason)
	assert.Equal(t, ShadowFilterAbilityDisabled, findRoutePreviewEntry(t, preview, abilityDisabledChannelID).FilterReason)
	assert.Equal(t, ShadowFilterSnapshotUnavailable, findRoutePreviewEntry(t, preview, missingSnapshotChannelID).FilterReason)
}

func TestRouteProfilePreviewHonorsTokenModelAndPathRestrictions(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "主线路", Enabled: true, Entries: []RouteEntryInput{{
				ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
			}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"model_limits_enabled": true,
		"model_limits":         "claude-test",
	}).Error)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, ShadowFilterTokenForbidden, findRoutePreviewEntry(t, preview, channelID).FilterReason)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("model_limits_enabled", false).Error)
	preview, err = PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/messages",
	})
	require.NoError(t, err)
	assert.Equal(t, ShadowFilterPathUnsupported, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewRejectsDisabledOrExpiredToken(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "令牌状态", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("status", common.TokenStatusDisabled).Error)
	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, ShadowFilterTokenForbidden, findRoutePreviewEntry(t, preview, channelID).FilterReason)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"status": common.TokenStatusEnabled, "expired_time": common.GetTimestamp() - 1,
	}).Error)
	preview, err = PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, ShadowFilterTokenForbidden, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewUsesNormalizedModelForTokenLimits(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "规范化模型", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"model_limits_enabled": true,
		"model_limits":         "gpt-test",
	}).Error)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "ＧＰＴ-ＴＥＳＴ", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, "gpt-test", preview.NormalizedModel)
	assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
	assert.Empty(t, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewRejectsUnknownCapabilityState(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Model(&model.ChannelModelCapability{}).
		Where("channel_id = ?", channelID).Update("state", "future_state").Error)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "未知状态", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, ShadowFilterUnknownCapability, findRoutePreviewEntry(t, preview, channelID).FilterReason)
}

func TestRouteProfilePreviewHandlesEmptyAndForeignProfiles(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, _ := seedRouteProfileFixture(t, db)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Nil(t, preview.ActiveGroup)
	assert.Equal(t, 1, preview.FilterReasonCounts[RoutePreviewFilterActiveGroupMissing])

	_, err = PreviewUserRouteProfile(context.Background(), userID+1, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	assert.True(t, errors.Is(err, ErrRouteProfileNotFound))
}

func TestRouteProfileUpdateKeepsExistingRevokedEntryForRemoval(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "主线路", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{
		UserID: userID, ChannelID: channelID, Source: model.RouteSourcePlatform,
		Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id: tokenID + 1, UserId: userID, Key: "routing-second-token", Name: "routing-second-token",
		Status: common.TokenStatusEnabled, Group: "default", ExpiredTime: -1, UnlimitedQuota: true,
	}).Error)

	updated, err := UpdateUserRouteProfile(created.Profile.ID, RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual, Version: created.Profile.Version,
		ActiveGroupID: created.Profile.ActiveGroupID,
		Groups: []RouteGroupInput{{
			ID: created.Groups[0].Group.ID, Name: "已撤销渠道", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, "已撤销渠道", updated.Groups[0].Group.Name)

	_, err = CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID + 1, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "新配置", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	assert.True(t, errors.Is(err, ErrRouteProfileValidation))
}

func TestRouteProfilePreviewUsesUnresolvedStateForMissingSnapshot(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "missing snapshot", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	entry := findRoutePreviewEntry(t, preview, channelID)
	assert.Equal(t, model.RouteCapabilityStateUnresolved, entry.CapabilityState)
	assert.Equal(t, ShadowFilterSnapshotUnavailable, entry.FilterReason)
}

func TestRouteProfileUpdateCannotModifyOrDeleteSystemAutoLabGroups(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "manual", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	manual := created.Groups[0].Group
	auto := model.UserRouteGroup{ProfileID: created.Profile.ID, Name: "system lab", Kind: model.RouteGroupKindAutoLab, Enabled: true, Position: 1}
	require.NoError(t, db.Create(&auto).Error)

	_, err = UpdateUserRouteProfile(created.Profile.ID, RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual, Version: created.Profile.Version,
		ActiveGroupID: &manual.ID,
		Groups: []RouteGroupInput{{
			ID: auto.ID, Name: "tampered", Kind: model.RouteGroupKindManual, Enabled: false, Position: 0,
		}},
	})
	assert.ErrorIs(t, err, ErrRouteProfileForbidden)

	updated, err := UpdateUserRouteProfile(created.Profile.ID, RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual, Version: created.Profile.Version,
		ActiveGroupID: &manual.ID,
		Groups: []RouteGroupInput{{
			ID: manual.ID, Name: "manual updated", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	assert.Len(t, updated.Groups, 2)
	var stored model.UserRouteGroup
	require.NoError(t, db.Where("id = ?", auto.ID).First(&stored).Error)
	assert.Equal(t, model.RouteGroupKindAutoLab, stored.Kind)
	assert.Equal(t, "system lab", stored.Name)
	assert.ErrorIs(t, DeleteUserRouteProfile(userID, created.Profile.ID), ErrRouteProfileForbidden)
}

func TestDuplicateRouteProfileErrorsAreNormalized(t *testing.T) {
	assert.True(t, isDuplicateRouteProfileError(errors.New("UNIQUE constraint failed: user_route_profiles.token_id")))
	assert.True(t, isDuplicateRouteProfileError(errors.New("duplicate key value violates unique constraint")))
	assert.True(t, isDuplicateRouteProfileError(errors.New("Duplicate entry for key token_id")))
	assert.False(t, isDuplicateRouteProfileError(errors.New("database unavailable")))
}

func TestDeleteRouteProfileCascadesChildren(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "主线路", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, DeleteUserRouteProfile(userID, created.Profile.ID))

	for _, target := range []any{&model.UserRouteProfile{}, &model.UserRouteGroup{}, &model.UserRouteEntry{}, &model.RoutePolicy{}} {
		var count int64
		require.NoError(t, db.Model(target).Count(&count).Error)
		assert.Zero(t, count)
	}
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
