package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutingAPIIsFailClosedByDefault(t *testing.T) {
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "false")
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 1)

	ListRouteProfiles(c)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "feature_disabled", response.Code)
}

func TestFrontendRoutingCapabilityRemainsDisabledUntilSelectorIsLive(t *testing.T) {
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "false")
	assert.Equal(t, "disabled", routingCapabilityStatus())
}

func TestRoutingRejectsMalformedProfileIDAsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "not-an-id"}}

	profileID, ok := parseRouteProfileID(c)

	assert.False(t, ok)
	assert.Zero(t, profileID)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"BAD_REQUEST"`)
}

func TestListEligibleRouteChannelsReportsOnlyActiveCapabilitySnapshot(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.Ability{}, &model.UserChannelEntitlement{},
		&model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{},
	))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	const userID = 4101
	const activeChannelID = 41011
	const snapshotlessChannelID = 41012
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "routing-eligible-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}).Error)
	for _, channelID := range []int{activeChannelID, snapshotlessChannelID} {
		require.NoError(t, db.Create(&model.Channel{
			Id: channelID, Key: fmt.Sprintf("routing-eligible-key-%d", channelID), Name: fmt.Sprintf("routing-eligible-%d", channelID),
			Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default",
		}).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "default", Model: "gpt-test", Enabled: true}).Error)
	}
	groups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpoints, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), activeChannelID, model.ChannelCapabilitySnapshotFence{}, "eligible-source", "eligible-catalog", []model.ChannelModelCapability{{
		RequestModel: "gpt-test", ActualModel: "gpt-test", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))
	activeFence, err := model.GetChannelCapabilitySnapshotFence(context.Background(), activeChannelID)
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), activeChannelID, activeFence, "eligible-source-v2", "eligible-catalog-v2", []model.ChannelModelCapability{{
		RequestModel: "gpt-test-v2", ActualModel: "gpt-test-v2", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", activeChannelID).Update("model", "gpt-test-v2").Error)

	channels, err := listEligibleRouteChannels(userID)
	require.NoError(t, err)
	byID := make(map[int]eligibleRouteChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	assert.Equal(t, int64(2), byID[activeChannelID].SnapshotVersion)
	assert.Equal(t, "eligible-catalog-v2", byID[activeChannelID].CatalogVersion)
	assert.Equal(t, model.RouteCapabilityStateEligible, byID[activeChannelID].CapabilityState)
	assert.Equal(t, []string{"gpt-test-v2"}, byID[activeChannelID].RequestModels)
	assert.Zero(t, byID[activeChannelID].FilterReason)
	assert.Zero(t, byID[snapshotlessChannelID].SnapshotVersion)
	assert.Equal(t, model.RouteCapabilityStateUnresolved, byID[snapshotlessChannelID].CapabilityState)
	assert.Equal(t, service.ShadowFilterSnapshotUnavailable, byID[snapshotlessChannelID].FilterReason)
}

func TestRouteCatalogFiltersCapabilitiesWithoutCurrentModelAbility(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.Ability{}, &model.UserChannelEntitlement{},
		&model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{},
	))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")

	const userID = 4201
	const channelID = 42011
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "catalog-access-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Key: "catalog-access-key", Name: "catalog-access", Status: common.ChannelStatusEnabled, Models: "gpt-visible,gpt-hidden", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "default", Model: "gpt-visible", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "default", Model: "gpt-hidden", Enabled: false}).Error)
	groups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpoints, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, "catalog-access-source", "catalog-access-v1", []model.ChannelModelCapability{
		{RequestModel: "gpt-hidden", ActualModel: "gpt-hidden", LabSlug: "openai", Source: "canonical", Confidence: 1, AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled, ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible},
		{RequestModel: "gpt-visible", ActualModel: "gpt-visible", LabSlug: "openai", Source: "canonical", Confidence: 1, AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled, ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible},
	}))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", userID)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/routing/catalog", nil)
	ListRouteCatalog(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"request_model":"gpt-visible"`)
	assert.NotContains(t, recorder.Body.String(), "gpt-hidden")
}

func TestUpdateChannelRoutePolicyRejectsQueryBodyModelMismatch(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelRoutePolicy{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Create(&model.Channel{Id: 4201, Name: "route-policy-test", Key: "redacted-test-key", Status: common.ChannelStatusEnabled}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "4201"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/channel/4201/route-policy?model=gpt-test", strings.NewReader(`{"canonical_model":"claude-test","enabled":true,"max_channel_concurrency":1}`))
	c.Request.Header.Set("Content-Type", "application/json")

	UpdateChannelRoutePolicy(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"MODEL_MISMATCH"`)
}

func TestChannelRoutePolicyUpdateMapsVersionConflict(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelRoutePolicy{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Create(&model.Channel{Id: 4202, Name: "route-policy-conflict", Key: "redacted-test-key"}).Error)
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: 4202, CanonicalModel: "gpt-test", MaxChannelConcurrency: 1, Enabled: true, Version: 1,
	}).Error)
	_, err = service.SaveChannelRoutePolicy(model.ChannelRoutePolicy{
		ChannelID: 4202, CanonicalModel: "gpt-test", MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "4202"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/channel/4202/route-policy?model=gpt-test", strings.NewReader(`{"canonical_model":"gpt-test","enabled":true,"max_channel_concurrency":3,"version":1}`))
	c.Request.Header.Set("Content-Type", "application/json")

	UpdateChannelRoutePolicy(c)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"VERSION_CONFLICT"`)
}

func TestGetChannelRoutePolicyDoesNotExposeSensitiveChannelFields(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelRoutePolicy{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Create(&model.Channel{Id: 4203, Name: "route-policy-safe", Key: "redacted-test-key"}).Error)
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: 4203, CanonicalModel: "gpt-test", MaxChannelConcurrency: 1, Enabled: true, Version: 1,
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "4203"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/4203/route-policy?model=gpt-test", nil)

	GetChannelRoutePolicy(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, `"canonical_model":"gpt-test"`)
	assert.NotContains(t, body, `"key"`)
	assert.NotContains(t, body, `"base_url"`)
	assert.NotContains(t, body, `"header_override"`)
}

func TestChannelRoutePolicyRequiresModelQuery(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "4204"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/4204/route-policy", nil)

	GetChannelRoutePolicy(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"BAD_REQUEST"`)
}
