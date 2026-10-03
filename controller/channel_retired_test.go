package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRetiredChannelManagementFailsWithoutWritesOrRequests(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	legacy := model.Channel{Id: 57, Type: constant.ChannelTypeCodex, Name: "historical subscription", Key: "fixture-secret", Models: "gpt-5-codex", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, ChannelRatio: "1"}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&legacy).Error)
	active := model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "official API", Key: "official-fixture", Status: common.ChannelStatusEnabled, ChannelRatio: "1"}
	require.NoError(t, db.Create(&active).Error)
	tests := []struct {
		name, method, body string
		handler            gin.HandlerFunc
	}{
		{"create", http.MethodPost, `{"mode":"single","channel":{"type":57,"key":"fixture-secret","models":"gpt-5-codex"}}`, AddChannel},
		{"edit omitted type", http.MethodPut, `{"id":57,"name":"changed"}`, UpdateChannel},
		{"convert historical row", http.MethodPut, `{"id":57,"type":1,"name":"changed"}`, UpdateChannel},
		{"convert active row", http.MethodPut, `{"id":1,"type":57}`, UpdateChannel},
		{"copy", http.MethodPost, ``, CopyChannel},
		{"key", http.MethodPost, ``, GetChannelKey},
		{"test", http.MethodGet, ``, TestChannel},
		{"fetch saved models", http.MethodGet, ``, FetchUpstreamModels},
		{"fetch draft models", http.MethodPost, `{"type":57,"key":"fixture-secret"}`, FetchModels},
		{"fetch saved preview", http.MethodPost, `{"channel_id":57,"type":58}`, FetchModels},
		{"sync detect", http.MethodPost, `{"id":57}`, DetectChannelUpstreamModelUpdates},
		{"sync apply", http.MethodPost, `{"id":57,"add_models":["gpt-5"]}`, ApplyChannelUpstreamModelUpdates},
		{"status", http.MethodPost, `{"status":2}`, UpdateChannelStatus},
		{"mixed batch status", http.MethodPost, `{"ids":[1,57],"status":2}`, BatchUpdateChannelStatus},
		{"multi key", http.MethodPost, `{"channel_id":57,"action":"get_key_status"}`, ManageMultiKeys},
		{"balance", http.MethodGet, ``, UpdateChannelBalance},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "id", Value: "57"}}
			c.Request = httptest.NewRequest(test.method, "/api/channel/57", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			test.handler(c)
			var result struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result), recorder.Body.String())
			assert.False(t, result.Success, recorder.Body.String())
			assert.Contains(t, result.Message, constant.ErrRetiredChannelType.Error())
			assert.NotContains(t, recorder.Body.String(), "fixture-secret")
			var rows []model.Channel
			require.NoError(t, db.Order("id").Find(&rows).Error)
			require.Len(t, rows, 2)
			assert.Equal(t, active.Type, rows[0].Type)
			assert.Equal(t, active.Name, rows[0].Name)
			assert.Equal(t, common.ChannelStatusEnabled, rows[0].Status)
			assert.Equal(t, legacy.Type, rows[1].Type)
			assert.Equal(t, legacy.Name, rows[1].Name)
			assert.Equal(t, common.ChannelStatusEnabled, rows[1].Status, "retirement never rewrites legacy data")
			assert.Zero(t, sends.Load())
		})
	}
	assert.ErrorIs(t, testChannel(context.Background(), &legacy, 1, "gpt-5-codex", "", true).localErr, constant.ErrRetiredChannelType)
}

func TestRetiredChannelListProjectsLifecycleWithoutRewriting(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	legacy := model.Channel{Id: 57, Type: constant.ChannelTypeCodex, Name: "historical", Key: "fixture-secret", Models: "gpt-5-codex", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&legacy).Error)
	for name, item := range map[string]any{"legacy": buildLegacyChannelDTO(&legacy), "next": buildNextAdminChannelDTO(&legacy)} {
		t.Run(name, func(t *testing.T) {
			raw, err := common.Marshal(item)
			require.NoError(t, err)
			var output map[string]any
			require.NoError(t, common.Unmarshal(raw, &output))
			assert.Equal(t, "retired", output["lifecycle"])
			assert.Equal(t, "reserved", output["category"])
			assert.Equal(t, float64(common.ChannelStatusManuallyDisabled), output["status"])
			assert.Equal(t, constant.ErrRetiredChannelType.Error(), output["status_reason"])
		})
	}
	assert.Equal(t, common.ChannelStatusEnabled, legacy.Status)
	for filter, want := range map[int]int64{common.ChannelStatusEnabled: 0, 0: 1, -1: 1} {
		var count int64
		require.NoError(t, buildChannelListQuery("", filter, -1).Count(&count).Error)
		assert.Equal(t, want, count, fmt.Sprint(filter))
	}
}

func TestRetiredChannelIsExcludedFromEligibleRouteChannels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	const userID = 4301
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "retired-eligible-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}).Error)
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&[]model.Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "official API", Key: "official-fixture", Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default"},
		{Id: 57, Type: constant.ChannelTypeCodex, Name: "historical", Key: "fixture-secret", Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default"},
	}).Error)
	groups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpoints, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	for _, channelID := range []int{1, 57} {
		require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "default", Model: "gpt-test", Enabled: true}).Error)
		// The retired row keeps a snapshot published before its type was retired.
		require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, fmt.Sprintf("source-%d", channelID), "catalog", []model.ChannelModelCapability{{
			RequestModel: "gpt-test", ActualModel: "gpt-test", LabSlug: "openai", Source: "canonical", Confidence: 1,
			AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
			ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
		}}))
	}
	channels, err := listEligibleRouteChannels(userID)
	require.NoError(t, err)
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.ID)
	}
	assert.Equal(t, []int{1}, ids)
}

func TestRetiredChannelSearchStatusFilterMatchesList(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&[]model.Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "search-active", Key: "official-fixture", Status: common.ChannelStatusEnabled},
		{Id: 57, Type: constant.ChannelTypeCodex, Name: "search-historical", Key: "fixture-secret", Status: common.ChannelStatusEnabled},
	}).Error)
	for status, wantIDs := range map[string][]float64{"enabled": {1}, "disabled": {57}} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/search?keyword=search&status="+status, nil)
		SearchChannels(c)
		var result struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result), recorder.Body.String())
		ids := make([]float64, 0, len(result.Data.Items))
		for _, item := range result.Data.Items {
			ids = append(ids, item["id"].(float64))
		}
		assert.Equal(t, wantIDs, ids, status)
	}
}

func TestRetiredChannelIsExcludedFromAutomaticProbes(t *testing.T) {
	legacy := &model.Channel{Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled}
	active := &model.Channel{Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	assert.Equal(t, []*model.Channel{active}, selectChannelsForAutomaticTest([]*model.Channel{legacy, active}, operation_setting.ChannelTestModeScheduledAll))
}
