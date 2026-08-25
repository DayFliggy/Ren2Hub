package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	t.Setenv("NEXT_TOKEN_PRIVATE_ROUTING_ENABLED", "false")
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

	channels, err := listEligibleRouteChannels(userID)
	require.NoError(t, err)
	byID := make(map[int]eligibleRouteChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	assert.Equal(t, int64(1), byID[activeChannelID].SnapshotVersion)
	assert.Equal(t, "eligible-catalog", byID[activeChannelID].CatalogVersion)
	assert.Equal(t, model.RouteCapabilityStateEligible, byID[activeChannelID].CapabilityState)
	assert.Zero(t, byID[activeChannelID].FilterReason)
	assert.Zero(t, byID[snapshotlessChannelID].SnapshotVersion)
	assert.Equal(t, service.ShadowFilterSnapshotUnavailable, byID[snapshotlessChannelID].FilterReason)
}
