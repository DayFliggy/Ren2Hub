package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func buildAffinityBindingContext(t *testing.T, affinityValue, credential string, channelID int) *gin.Context {
	t.Helper()
	newContext := func() *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		ctx.Request.Header.Set("X-Affinity-Key", affinityValue)
		common.SetContextKey(ctx, constant.ContextKeyUserId, 101)
		common.SetContextKey(ctx, constant.ContextKeyTokenId, 202)
		return ctx
	}

	recordContext := newContext()
	_, found := service.GetPreferredChannelByAffinity(recordContext, "gpt-5")
	require.False(t, found)
	common.SetContextKey(recordContext, constant.ContextKeyChannelKey, credential)
	service.RecordChannelAffinity(recordContext, channelID)
	t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(recordContext) })

	hitContext := newContext()
	preferredChannelID, found := service.GetPreferredChannelByAffinity(hitContext, "gpt-5")
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)
	service.MarkChannelAffinityUsed(hitContext, preferredChannelID)
	return hitContext
}

func installAffinityRuleForTest(t *testing.T, skipRetry bool) operation_setting.ChannelAffinityRule {
	t.Helper()
	rule := operation_setting.ChannelAffinityRule{
		Name:               fmt.Sprintf("middleware-binding-%d", time.Now().UnixNano()),
		ModelRegex:         []string{"^gpt-5$"},
		PathRegex:          []string{"/v1/responses"},
		KeySources:         []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Affinity-Key"}},
		SkipRetryOnFailure: skipRetry,
		IncludeRuleName:    true,
		IncludeModelName:   true,
	}
	setting := operation_setting.GetChannelAffinitySetting()
	originalRules := setting.Rules
	setting.Rules = append([]operation_setting.ChannelAffinityRule{rule}, originalRules...)
	t.Cleanup(func() { setting.Rules = originalRules })
	return rule
}

func affinityMultiKeyChannel(channelID int, secondStatus int) *model.Channel {
	return &model.Channel{
		Id:            channelID,
		Type:          constant.ChannelTypeOpenAI,
		Key:           "credential-a\ncredential-b",
		Keys:          []string{"credential-a", "credential-b"},
		CapacityTotal: model.DefaultChannelCapacityTotal,
		ChannelRatio:  model.DefaultChannelRatio,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusEnabled,
				1: secondStatus,
			},
		},
	}
}

func installAffinityRouteHealthDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
	return db
}

func setAffinityLiveRoute(ctx *gin.Context) {
	ctx.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab})
}

func TestSetupContextForSelectedChannelUsesBoundAffinityCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, true)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("bound-%d", time.Now().UnixNano()), "credential-b", 301)

	err := SetupContextForSelectedChannel(ctx, affinityMultiKeyChannel(301, common.ChannelStatusEnabled), "gpt-5")
	require.Nil(t, err)
	require.Equal(t, "credential-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, 1, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
}

func TestSetupContextForSelectedChannelRejectsUnavailableExactAffinityCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, true)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("strict-%d", time.Now().UnixNano()), "credential-b", 302)

	err := SetupContextForSelectedChannel(ctx, affinityMultiKeyChannel(302, common.ChannelStatusManuallyDisabled), "gpt-5")
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
}

func TestSetupContextForSelectedChannelMigratesReconstructibleAffinity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, false)
	affinityValue := fmt.Sprintf("migrate-%d", time.Now().UnixNano())
	ctx := buildAffinityBindingContext(t, affinityValue, "credential-b", 303)

	err := SetupContextForSelectedChannel(ctx, affinityMultiKeyChannel(303, common.ChannelStatusManuallyDisabled), "gpt-5")
	require.Nil(t, err)
	require.Equal(t, "credential-a", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, 0, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))

	recorder := httptest.NewRecorder()
	lookupContext, _ := gin.CreateTestContext(recorder)
	lookupContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	lookupContext.Request.Header.Set("X-Affinity-Key", affinityValue)
	common.SetContextKey(lookupContext, constant.ContextKeyUserId, 101)
	common.SetContextKey(lookupContext, constant.ContextKeyTokenId, 202)
	_, found := service.GetPreferredChannelByAffinity(lookupContext, "gpt-5")
	require.False(t, found)
}

func TestSetupContextForSelectedChannelClaimsRecoveredBoundKeyHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := installAffinityRouteHealthDB(t)
	installAffinityRuleForTest(t, true)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("recovered-%d", time.Now().UnixNano()), "credential-b", 308)
	setAffinityLiveRoute(ctx)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID:     308,
		Model:         "gpt-5",
		KeyScope:      service.RouteKeyScope("credential-b"),
		State:         model.RouteHealthStateOpen,
		CooldownUntil: time.Now().Add(-time.Minute).Unix(),
		HealthEpoch:   4,
	}).Error)

	err := SetupContextForSelectedChannel(ctx, affinityMultiKeyChannel(308, common.ChannelStatusEnabled), "gpt-5")
	require.Nil(t, err)
	require.Equal(t, "credential-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))

	var health model.ChannelHealth
	require.NoError(t, db.Where("channel_id = ? AND model = ? AND key_scope = ?", 308, "gpt-5", service.RouteKeyScope("credential-b")).First(&health).Error)
	require.Equal(t, model.RouteHealthStateHalfOpen, health.State)
	require.EqualValues(t, 5, health.HealthEpoch)
}

func TestSetupContextForSelectedChannelRejectsOpenExactAffinityKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := installAffinityRouteHealthDB(t)
	installAffinityRuleForTest(t, true)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("open-strict-%d", time.Now().UnixNano()), "credential-b", 309)
	setAffinityLiveRoute(ctx)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID:     309,
		Model:         "gpt-5",
		KeyScope:      service.RouteKeyScope("credential-b"),
		State:         model.RouteHealthStateOpen,
		CooldownUntil: time.Now().Add(time.Minute).Unix(),
		HealthEpoch:   2,
	}).Error)

	err := SetupContextForSelectedChannel(ctx, affinityMultiKeyChannel(309, common.ChannelStatusEnabled), "gpt-5")
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
}

func TestSetupContextForSelectedChannelMigratesFromOpenAffinityKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := installAffinityRouteHealthDB(t)
	installAffinityRuleForTest(t, false)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("open-migrate-%d", time.Now().UnixNano()), "credential-b", 310)
	setAffinityLiveRoute(ctx)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID:     310,
		Model:         "gpt-5",
		KeyScope:      service.RouteKeyScope("credential-b"),
		State:         model.RouteHealthStateOpen,
		CooldownUntil: time.Now().Add(time.Minute).Unix(),
		HealthEpoch:   2,
	}).Error)

	err := SetupContextForSelectedChannel(ctx, affinityMultiKeyChannel(310, common.ChannelStatusEnabled), "gpt-5")
	require.Nil(t, err)
	require.Equal(t, "credential-a", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
}

func TestSingleKeyChannelRecordsAffinityCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, true)
	affinityValue := fmt.Sprintf("single-key-%d", time.Now().UnixNano())

	recorder := httptest.NewRecorder()
	recordContext, _ := gin.CreateTestContext(recorder)
	recordContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordContext.Request.Header.Set("X-Affinity-Key", affinityValue)
	common.SetContextKey(recordContext, constant.ContextKeyUserId, 101)
	common.SetContextKey(recordContext, constant.ContextKeyTokenId, 202)
	_, found := service.GetPreferredChannelByAffinity(recordContext, "gpt-5")
	require.False(t, found)

	channel := &model.Channel{
		Id:            311,
		Type:          constant.ChannelTypeOpenAI,
		Key:           "single-credential",
		CapacityTotal: model.DefaultChannelCapacityTotal,
		ChannelRatio:  model.DefaultChannelRatio,
	}
	require.Nil(t, SetupContextForSelectedChannel(recordContext, channel, "gpt-5"))
	require.Equal(t, "single-credential", common.GetContextKeyString(recordContext, constant.ContextKeyChannelKey))
	service.RecordChannelAffinity(recordContext, channel.Id)
	t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(recordContext) })

	hitRecorder := httptest.NewRecorder()
	hitContext, _ := gin.CreateTestContext(hitRecorder)
	hitContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	hitContext.Request.Header.Set("X-Affinity-Key", affinityValue)
	common.SetContextKey(hitContext, constant.ContextKeyUserId, 101)
	common.SetContextKey(hitContext, constant.ContextKeyTokenId, 202)
	preferredChannelID, found := service.GetPreferredChannelByAffinity(hitContext, "gpt-5")
	require.True(t, found)
	require.Equal(t, channel.Id, preferredChannelID)
	service.MarkChannelAffinityUsed(hitContext, preferredChannelID)
	key, index, bound, available := service.ResolveChannelAffinityCredential(hitContext, channel, nil)
	require.True(t, bound)
	require.True(t, available)
	require.Equal(t, "single-credential", key)
	require.Zero(t, index)
}

func TestResolveChannelAffinityCredentialUsesEnabledDuplicate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, true)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("duplicate-%d", time.Now().UnixNano()), "credential-b", 312)
	channel := affinityMultiKeyChannel(312, common.ChannelStatusEnabled)
	channel.Key = "credential-b\ncredential-b"
	channel.Keys = []string{"credential-b", "credential-b"}
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusEnabled}

	key, index, bound, available := service.ResolveChannelAffinityCredential(ctx, channel, nil)
	require.True(t, bound)
	require.True(t, available)
	require.Equal(t, "credential-b", key)
	require.Equal(t, 1, index)
}

func TestEnforceSelectedChannelAffinityRejectsExactChannelMigration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, true)
	ctx := buildAffinityBindingContext(t, fmt.Sprintf("strict-channel-%d", time.Now().UnixNano()), "credential-a", 304)

	err := enforceSelectedChannelAffinity(ctx, 304, 305)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
}

func TestEnforceSelectedChannelAffinityClearsReconstructibleChannelBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	installAffinityRuleForTest(t, false)
	affinityValue := fmt.Sprintf("migrate-channel-%d", time.Now().UnixNano())
	ctx := buildAffinityBindingContext(t, affinityValue, "credential-a", 306)

	require.Nil(t, enforceSelectedChannelAffinity(ctx, 306, 307))

	recorder := httptest.NewRecorder()
	lookupContext, _ := gin.CreateTestContext(recorder)
	lookupContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	lookupContext.Request.Header.Set("X-Affinity-Key", affinityValue)
	common.SetContextKey(lookupContext, constant.ContextKeyUserId, 101)
	common.SetContextKey(lookupContext, constant.ContextKeyTokenId, 202)
	_, found := service.GetPreferredChannelByAffinity(lookupContext, "gpt-5")
	require.False(t, found)
}

func TestGetModelFromJSONBodyPublishesContinuationRequirement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-5",
		"previous_response_id":"resp_123",
		"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := getModelFromJSONBody(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", request.Model)
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyRequestContinuation))

	value, exists := ctx.Get(requestRequirementFeaturesContextKey)
	require.True(t, exists)
	features, ok := value.(service.RequestRequirementFeatures)
	require.True(t, ok)
	require.True(t, features.Continuation)
	require.Equal(t, []string{"image"}, features.InputModalities)
}
