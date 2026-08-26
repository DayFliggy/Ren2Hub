package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
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

func TestLiveRouteRuntimeQualificationRequiresPriceAndSecurityFacts(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, qualified := liveRouteRuntimeQualificationForRequest(c)
	assert.False(t, qualified)

	markLiveRouteSecurityQualified(c)
	_, qualified = liveRouteRuntimeQualificationForRequest(c)
	assert.False(t, qualified)

	markLiveRoutePriceQualified(c)
	facts, qualified := liveRouteRuntimeQualificationForRequest(c)
	assert.True(t, qualified)
	assert.True(t, facts.PriceEligibilityKnown)
	assert.True(t, facts.PriceEligible)
	assert.True(t, facts.SecurityEligibilityKnown)
	assert.True(t, facts.SecurityAllowed)
}

func TestRelayResponseCommittedDoesNotTreatUnparsedFirstFrameAsOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{StartTime: time.Now().Add(-time.Second)}
	info.SetFirstResponseTime()

	assert.False(t, relayResponseCommitted(c, info))
	info.MarkValidOutput()
	assert.True(t, relayResponseCommitted(c, info))

	info = &relaycommon.RelayInfo{StartTime: time.Now().Add(-time.Second)}
	_, _ = c.Writer.Write([]byte("response"))
	assert.True(t, relayResponseCommitted(c, info))
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
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")
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

func TestSetupContextLiveRouteClaimsOnlyOneRecoveredKeyProbe(t *testing.T) {
	t.Setenv("ROUTE_LIVE_ENABLED", "true")
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")
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

	channel := model.Channel{Id: 92012, Type: constant.ChannelTypeOpenAI, Key: "recovering-key", Name: "single-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channel.Id, Model: "gpt-test", KeyScope: service.RouteKeyScope("recovering-key"), State: model.RouteHealthStateOpen,
		FailureCount: 1, CooldownUntil: time.Now().Add(-time.Second).Unix(), HealthEpoch: 4,
	}).Error)

	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab})
		return c
	}

	first := newContext()
	require.Nil(t, middleware.SetupContextForSelectedChannel(first, &channel, "gpt-test"))
	second := newContext()
	setupErr := middleware.SetupContextForSelectedChannel(second, &channel, "gpt-test")
	require.NotNil(t, setupErr)
	assert.Equal(t, types.ErrorCodeChannelNoAvailableKey, setupErr.GetErrorCode())
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

func TestNextLiveRouteCandidateIndexAdvancesExactlyOnce(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source: service.RouteSourceAutoLab,
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: 101}, {ChannelID: 102}, {ChannelID: 103},
		},
	})

	next, found := nextLiveRouteCandidateIndex(c, 0)
	require.True(t, found)
	assert.Equal(t, 1, next)

	next, found = nextLiveRouteCandidateIndex(c, next)
	require.True(t, found)
	assert.Equal(t, 2, next)
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
	markLiveRouteSecurityQualified(c)
	markLiveRoutePriceQualified(c)
	info := &relaycommon.RelayInfo{RequestId: "lease-qualification-request", UserId: userID, TokenId: token.Id, UserGroup: "default", OriginModelName: "gpt-test"}

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
	assert.True(t, c.GetBool(liveRouteRenewalFailedKey))
}

func TestLiveRouteRenewalFailureIsAdmissionError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_renewal", service.RouteLeaseRenewal{
		Failure: func() error { return service.ErrRouteLeaseUnavailable },
	})

	assert.True(t, liveRouteRenewalFailed(c))
	classification := service.ClassifyRouteError(http.StatusServiceUnavailable, service.RouteLeaseFailureCode, service.ErrRouteLeaseUnavailable.Error(), false)
	assert.Equal(t, service.RouteErrorAdmission, classification.Class)
	assert.False(t, classification.Failoverable)
}
