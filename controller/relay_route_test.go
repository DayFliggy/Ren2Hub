package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLiveRoutePriceRatioAllowedOnlyUsesManualPolicy(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 2))

	ctx.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceManual,
		MaxRatio: 1.5,
	})
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 1.5))
	assert.False(t, liveRoutePriceRatioAllowed(ctx, 2))

	ctx.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceAutoLab,
		MaxRatio: 1,
	})
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 2))
}

func TestLiveRouteQualificationFactsUseCalculatedPriceAndEstablishedSecurity(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceManual,
		MaxRatio: 1.5,
	})
	info := &relaycommon.RelayInfo{}
	info.PriceData.GroupRatioInfo.GroupRatio = 2
	priceKnown, priceEligible, securityKnown, securityAllowed := liveRouteQualificationFacts(c, info)
	assert.True(t, priceKnown)
	assert.False(t, priceEligible)
	assert.True(t, securityKnown)
	assert.True(t, securityAllowed)
}

func TestReleaseRejectedRouteAttemptLeaseReturnsReleaseFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceManual,
		Decision: service.RouteDecision{Candidates: []service.RouteDecisionCandidate{{ChannelID: 71}}},
	})
	cause := errors.New("qualification rejected")
	err := releaseRejectedRouteAttemptLease(c, service.RouteLease{
		LeaseID: "lease", RequestID: "request", ChannelID: 71,
		Resources: []service.RouteLeaseResource{{Key: "route:lease:test", Capacity: 1}},
	}, cause)
	assert.ErrorIs(t, err, cause)
	assert.ErrorIs(t, err, service.ErrRouteLeaseUnavailable)
	selection := c.MustGet("route_live_selection").(service.LiveRouteSelection)
	assert.Equal(t, service.RouteLeaseStateReleaseFailed, selection.Decision.LeaseState)
}

func TestLiveRouteFailoverAttemptCountsChannelChangesOnly(t *testing.T) {
	selection := service.LiveRouteSelection{
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: 11}, {ChannelID: 11}, {ChannelID: 12}, {ChannelID: 13},
		},
	}
	assert.Equal(t, 0, liveRouteFailoverAttempt(selection, 0))
	assert.Equal(t, 0, liveRouteFailoverAttempt(selection, 1))
	assert.Equal(t, 1, liveRouteFailoverAttempt(selection, 2))
	assert.Equal(t, 2, liveRouteFailoverAttempt(selection, 3))
}

func TestBeginLiveRouteUpstreamAttemptTracksIndependentBudgetsAndKeys(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 0)
	assert.Equal(t, 0, beginLiveRouteUpstreamAttempt(c, 11))

	common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 1)
	assert.Equal(t, 1, beginLiveRouteUpstreamAttempt(c, 11))
	assert.Equal(t, 2, beginLiveRouteUpstreamAttempt(c, 12))

	counters := liveRouteRetryCounters(c)
	assert.Equal(t, 3, counters.TotalAttempts)
	assert.Equal(t, 0, counters.SameKeyAttempts)
	assert.Equal(t, 1, counters.SameChannelAttempts)
	assert.Equal(t, 1, counters.FailoverAttempts)
	assert.Equal(t, map[int]struct{}{0: {}, 1: {}}, service.GetLiveRouteAttemptedKeyIndexes(c, 11))
}

func TestSetupContextLiveRetryUsesAnotherEnabledKey(t *testing.T) {
	t.Setenv("ROUTE_LIVE_ENABLED", "true")
	originalDB, originalMemoryCache := model.DB, common.MemoryCacheEnabled
	originalDatabaseType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.MemoryCacheEnabled = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCache
		common.SetMainDatabaseType(originalDatabaseType)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))

	first := model.Channel{
		Id: 92011, Type: constant.ChannelTypeOpenAI, Key: "first-key\nsecond-key", Name: "multi-key", Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeyMode: constant.MultiKeyModeRandom},
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	selection := service.LiveRouteSelection{Source: service.RouteSourceAutoLab, Attempts: []service.RouteDecisionCandidate{
		{ChannelID: first.Id}, {ChannelID: first.Id},
	}}
	c.Set("route_live_selection", selection)
	require.Nil(t, middleware.SetupContextForSelectedChannel(c, &first, "gpt-test"))
	initialKey := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	initialIndex := common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	beginLiveRouteUpstreamAttempt(c, first.Id)

	require.Nil(t, middleware.SetupContextForSelectedChannel(c, &first, "gpt-test"))
	assert.NotEqual(t, initialKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	assert.NotEqual(t, initialIndex, common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex))
}

func TestNextLiveRouteAttemptUsesErrorScope(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source: service.RouteSourceAutoLab,
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: 11}, {ChannelID: 11}, {ChannelID: 12},
		},
	})
	beginLiveRouteUpstreamAttempt(c, 11)

	keyFailure := service.ClassifyRouteError(401, "invalid_api_key", "", false)
	index, found := nextLiveRouteAttemptForError(c, 0, 11, keyFailure)
	require.True(t, found)
	assert.Equal(t, 1, index)

	modelFailure := service.ClassifyRouteError(400, "unsupported_model", "", false)
	index, found = nextLiveRouteAttemptForError(c, 0, 11, modelFailure)
	require.True(t, found)
	assert.Equal(t, 2, index)

	committedStream := service.CanRouteFailover(keyFailure, true, true)
	assert.False(t, committedStream)
}

func TestGetChannelSkipsExhaustedSingleKeyCandidate(t *testing.T) {
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")
	t.Setenv("ROUTE_LIVE_ENABLED", "true")
	originalDB, originalMemoryCache := model.DB, common.MemoryCacheEnabled
	originalDatabaseType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.MemoryCacheEnabled = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCache
		common.SetMainDatabaseType(originalDatabaseType)
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelHealth{}))

	first := model.Channel{Id: 92101, Type: constant.ChannelTypeOpenAI, Key: "first-key", Name: "first", Status: common.ChannelStatusEnabled}
	second := model.Channel{Id: 92102, Type: constant.ChannelTypeOpenAI, Key: "second-key", Name: "second", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source: service.RouteSourceAutoLab,
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: first.Id}, {ChannelID: first.Id}, {ChannelID: second.Id},
		},
	})
	service.RecordLiveRouteAttemptedKey(c, first.Id, 0)
	retry := 1
	param := &service.RetryParam{Ctx: c, Retry: &retry}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: first.Id},
		OriginModelName: "gpt-test",
	}

	selected, apiErr := getChannel(c, info, param)
	require.Nil(t, apiErr)
	require.NotNil(t, selected)
	assert.Equal(t, second.Id, selected.Id)
	assert.Equal(t, 2, param.GetRetry())
	assert.Equal(t, "second-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
}

func TestAcquireRouteAttemptLeaseReleasesCapacityAfterQualificationFailure(t *testing.T) {
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")
	t.Setenv("ROUTE_LIVE_ENABLED", "true")
	originalDB, originalRDB, originalRedisEnabled := model.DB, common.RDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	model.DB = db
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		model.DB, common.RDB, common.RedisEnabled = originalDB, originalRDB, originalRedisEnabled
		_ = client.Close()
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.UserRouteProfile{}, &model.UserRouteGroup{}, &model.UserRouteEntry{},
		&model.UserChannelEntitlement{}, &model.ChannelModelCapability{},
		&model.ChannelCapabilitySnapshot{}, &model.ChannelHealth{}, &model.ChannelRoutePolicy{},
	))

	const userID = 9101
	const channelID = 91011
	user := model.User{Id: userID, Username: "lease-qualification-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: userID, Key: "lease-qualification-token", Name: "lease-qualification-token", Status: common.TokenStatusEnabled, Group: "default", ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{Id: channelID, Type: constant.ChannelTypeOpenAI, Key: "lease-qualification-key", Name: "lease-qualification-channel", Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "default", Model: "gpt-test", Enabled: true}).Error)
	profile := model.UserRouteProfile{UserID: userID, TokenID: token.Id, Mode: model.RouteModeManual, Version: 1, Status: model.RouteProfileStatusEnabled}
	profile.Normalize(time.Now())
	require.NoError(t, db.Create(&profile).Error)
	group := model.UserRouteGroup{ProfileID: profile.ID, Name: "live", Kind: model.RouteGroupKindManual, Enabled: true}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&model.UserRouteEntry{GroupID: group.ID, ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}).Error)
	require.NoError(t, db.Model(&profile).Update("active_group_id", group.ID).Error)
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: channelID, CanonicalModel: "gpt-test", MaxUserConcurrency: 1,
		MaxTokenConcurrency: 1, MaxChannelConcurrency: 1, Enabled: true, Version: 1,
	}).Error)
	groups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpoints, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, "lease-source", "lease-catalog", []model.ChannelModelCapability{{
		RequestModel: "gpt-test", ActualModel: "gpt-test", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channelID).Update("enabled", false).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source: service.RouteSourceManual,
		Decision: service.RouteDecision{
			ConfigurationVersion: 1,
			Candidates: []service.RouteDecisionCandidate{{
				ChannelID: channelID, SnapshotVersion: 1, CatalogVersion: "lease-catalog", HealthEpoch: 1,
			}},
		},
		Attempts: []service.RouteDecisionCandidate{{
			ChannelID: channelID, SnapshotVersion: 1, CatalogVersion: "lease-catalog", HealthEpoch: 1,
		}},
	})
	info := &relaycommon.RelayInfo{RequestId: "lease-qualification-request", UserId: userID, TokenId: token.Id, UserGroup: "default", UsingGroup: "default", OriginModelName: "gpt-test"}

	err = acquireRouteAttemptLease(c, info, &channel, 0)
	assert.ErrorIs(t, err, service.ErrLiveRouteCandidateInvalid)
	assert.Equal(t, int64(0), client.ZCard(context.Background(), service.UserRouteLeaseKey(userID)).Val())
	assert.Equal(t, int64(0), client.ZCard(context.Background(), service.TokenRouteLeaseKey(token.Id)).Val())
	assert.Equal(t, int64(0), client.ZCard(context.Background(), service.ChannelModelRouteLeaseKey(channelID, "gpt-test")).Val())
	_, hasLease := c.Get("route_live_lease")
	assert.False(t, hasLease)
	updatedValue, ok := c.Get("route_live_selection")
	require.True(t, ok)
	updated := updatedValue.(service.LiveRouteSelection)
	assert.Equal(t, service.ShadowFilterAbilityDisabled, updated.Decision.Candidates[0].FilterReason)
	assert.Equal(t, "qualification_failed", updated.Decision.LeaseState)
}

func TestReleaseRouteAttemptLeaseStopsRenewalAndRestoresParentContext(t *testing.T) {
	originalRDB, originalRedisEnabled := common.RDB, common.RedisEnabled
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = originalRDB, originalRedisEnabled
		_ = client.Close()
	})

	resources := []service.RouteLeaseResource{{Key: "route:lease:test:release-order", Capacity: 1}}
	lease, err := service.AcquireRouteLease(context.Background(), client, "release-request", "release-lease", time.Minute, resources)
	require.NoError(t, err)
	lease.ChannelID = 31

	type contextKey string
	const parentKey contextKey = "parent"
	parentContext := context.WithValue(context.Background(), parentKey, "preserved")
	attemptContext, cancel := context.WithCancel(parentContext)
	renewal := service.StartRouteLeaseRenewal(attemptContext, client, lease, time.Millisecond, time.Minute)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(attemptContext)
	c.Set("route_live_lease", lease)
	c.Set("route_live_lease_cancel", context.CancelFunc(cancel))
	c.Set("route_live_lease_parent_context", parentContext)
	c.Set("route_live_renewal", renewal)

	releaseRouteAttemptLease(c)

	assert.NoError(t, c.Request.Context().Err())
	assert.Equal(t, "preserved", c.Request.Context().Value(parentKey))
	assert.Equal(t, int64(0), client.ZCard(context.Background(), resources[0].Key).Val())
	_, hasLease := c.Get("route_live_lease")
	assert.True(t, hasLease)
	assert.Nil(t, c.MustGet("route_live_lease"))
}

func TestReleaseRouteAttemptLeaseRecordsRenewalFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	selection := service.LiveRouteSelection{
		Source: service.RouteSourceAutoLab,
		Decision: service.RouteDecision{Candidates: []service.RouteDecisionCandidate{{
			ChannelID: 31, LeaseState: service.RouteLeaseStateAcquired,
		}}},
		Attempts: []service.RouteDecisionCandidate{{ChannelID: 31}},
	}
	c.Set("route_live_selection", selection)
	done := make(chan error)
	close(done)
	c.Set("route_live_renewal", service.RouteLeaseRenewal{
		Done:    done,
		Stop:    func() {},
		Failure: func() error { return service.ErrRouteLeaseUnavailable },
	})

	releaseRouteAttemptLease(c)
	updatedValue, ok := c.Get("route_live_selection")
	require.True(t, ok)
	updated := updatedValue.(service.LiveRouteSelection)
	assert.Equal(t, service.RouteLeaseStateRenewalFailed, updated.Decision.Candidates[0].LeaseState)
}

func TestLiveRouteLeaseRenewalFailureIsDetectableWithoutProviderError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_renewal", service.RouteLeaseRenewal{
		Failure: func() error { return service.ErrRouteLeaseUnavailable },
	})
	assert.True(t, liveRouteLeaseRenewalFailed(c))
	c.Set("route_live_renewal", service.RouteLeaseRenewal{})
	assert.False(t, liveRouteLeaseRenewalFailed(c))
}
