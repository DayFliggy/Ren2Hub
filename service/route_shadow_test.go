package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSelectRouteShadowUsesStaticPriorityAndHardFilters(t *testing.T) {
	endpointJSON, err := common.Marshal([]string{"openai"})
	require.NoError(t, err)
	oldIndex, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	t.Cleanup(func() { routeCapabilityIndex.Store(oldIndex) })
	routeCapabilityIndex.Store(&capabilityIndex{ByRequestModel: map[string][]indexedCapability{
		"gpt-5": {
			{
				Capability: model.ChannelModelCapability{
					ChannelID: 1, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai",
					State: model.RouteCapabilityStateEligible, SnapshotVersion: 1,
					CatalogVersion: "catalog-1", EndpointTypes: string(endpointJSON),
				},
				ChannelStatus: common.ChannelStatusEnabled, Priority: 20, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
			},
			{
				Capability: model.ChannelModelCapability{
					ChannelID: 2, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai",
					State: model.RouteCapabilityStateEligible, SnapshotVersion: 1,
					CatalogVersion: "catalog-1", EndpointTypes: string(endpointJSON),
				},
				ChannelStatus: common.ChannelStatusEnabled, Priority: 30, AbilityGroups: []string{"restricted"}, ChannelType: constant.ChannelTypeOpenAI,
			},
			{
				Capability: model.ChannelModelCapability{
					ChannelID: 3, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "",
					State: model.RouteCapabilityStateUnresolved, SnapshotVersion: 1,
					CatalogVersion: "catalog-1", EndpointTypes: string(endpointJSON),
				},
				ChannelStatus: common.ChannelStatusEnabled, Priority: 40, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
			},
		},
	}})

	decision := SelectRouteShadow(RouteShadowRequest{
		RequestID:     "req-shadow-filter",
		RequestModel:  "gpt-5",
		UserGroup:     "default",
		EndpointType:  "openai",
		PriceEligible: true, SecurityAllowed: true,
		Legacy: LegacySelectionTrace{SelectedChannelID: 1, PriorityLayers: map[int64][]int{20: {1}}},
	})

	assert.Equal(t, 1, decision.ShadowPreferredID)
	assert.Equal(t, ShadowReasonSameChannel, decision.DifferenceReasons[0])
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterGroupForbidden])
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterUnknownCapability])
	assert.True(t, decision.HasUnauthorized)
	assert.True(t, decision.HasUnknown)
}

func TestSelectRouteShadowStableTieBreakAndPathFilter(t *testing.T) {
	endpointJSON, err := common.Marshal([]string{"openai"})
	require.NoError(t, err)
	advanced := &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
		IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"gpt-5"},
	}}}
	oldIndex, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	t.Cleanup(func() { routeCapabilityIndex.Store(oldIndex) })
	routeCapabilityIndex.Store(&capabilityIndex{ByRequestModel: map[string][]indexedCapability{
		"gpt-5": {
			{Capability: model.ChannelModelCapability{ChannelID: 9, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON)}, ChannelStatus: common.ChannelStatusEnabled, Priority: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI},
			{Capability: model.ChannelModelCapability{ChannelID: 8, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON)}, ChannelStatus: common.ChannelStatusEnabled, Priority: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI},
			{Capability: model.ChannelModelCapability{ChannelID: 7, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON)}, ChannelStatus: common.ChannelStatusEnabled, Priority: 20, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeAdvancedCustom, Advanced: advanced},
		},
	}})

	decision := SelectRouteShadow(RouteShadowRequest{RequestModel: "gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true, RequestPath: "/v1/unsupported"})
	assert.Equal(t, 8, decision.ShadowPreferredID)
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterPathUnsupported])

	decision = SelectRouteShadow(RouteShadowRequest{RequestModel: "gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true, RequestPath: "/v1/chat/completions"})
	assert.Equal(t, 7, decision.ShadowPreferredID)
}

func TestShadowUsesActualModelLabForMappedRequests(t *testing.T) {
	endpointJSON, err := common.Marshal([]string{"openai"})
	require.NoError(t, err)
	oldIndex, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	t.Cleanup(func() { routeCapabilityIndex.Store(oldIndex) })
	routeCapabilityIndex.Store(&capabilityIndex{ByRequestModel: map[string][]indexedCapability{
		"public-model": {{
			Capability: model.ChannelModelCapability{
				ChannelID: 41, RequestModel: "public-model", ActualModel: "anthropic/claude-opus-5", LabSlug: "anthropic",
				State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON),
			},
			ChannelStatus: common.ChannelStatusEnabled, Priority: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
		}},
	}})

	decision := SelectRouteShadow(RouteShadowRequest{
		RequestModel: "public-model", UserGroup: "default", EndpointType: "openai",
		PriceEligible: true, SecurityAllowed: true,
	})
	assert.Equal(t, 41, decision.ShadowPreferredID)
	assert.Equal(t, "anthropic", decision.LabSlug)
	assert.Equal(t, "anthropic/claude-opus-5", decision.ActualModel)
}

func TestEndpointTypeForRequestPath(t *testing.T) {
	tests := map[string]string{
		"/v1/chat/completions":  string(constant.EndpointTypeOpenAI),
		"/v1/responses":         string(constant.EndpointTypeOpenAIResponse),
		"/v1/responses/compact": string(constant.EndpointTypeOpenAIResponseCompact),
		"/v1/messages":          string(constant.EndpointTypeAnthropic),
		"/v1beta/models/gemini": string(constant.EndpointTypeGemini),
		"/v1/unknown":           "",
	}
	for path, expected := range tests {
		assert.Equal(t, expected, endpointTypeForRequestPath(path), path)
	}
}

func TestRouteCapabilityProjectionSplitsMixedModels(t *testing.T) {
	channel := &model.Channel{
		Id: 101, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Models: "openai/gpt-5, anthropic/claude-opus-5", Group: "default",
	}
	capabilities := projectChannelCapabilities(channel, []model.Ability{
		{ChannelId: channel.Id, Group: "default", Model: "openai/gpt-5", Enabled: true},
		{ChannelId: channel.Id, Group: "default", Model: "anthropic/claude-opus-5", Enabled: true},
	}, modellab.DefaultCatalog(), "hash-1")

	require.Len(t, capabilities, 2)
	byLab := make(map[string]model.ChannelModelCapability, len(capabilities))
	for _, capability := range capabilities {
		byLab[capability.LabSlug] = capability
		assert.NotEqual(t, modellab.GroupMixed, capability.RequestModel)
	}
	assert.Equal(t, "openai", byLab["openai"].LabSlug)
	assert.Equal(t, "anthropic", byLab["anthropic"].LabSlug)
	assert.Equal(t, "hash-1", byLab["openai"].SourceHash)
}

func TestShadowDisabledDoesNotRecordMetrics(t *testing.T) {
	t.Setenv("ROUTE_SHADOW_ENABLED", "false")
	before := RouteShadowMetrics()
	RecordLegacySelectionAndShadow(context.Background(), RouteShadowRequest{
		RequestID: "req-shadow-disabled", RequestModel: "gpt-5", RequestPath: "/v1/chat/completions",
	}, "default", 0)
	after := RouteShadowMetrics()
	assert.Equal(t, before.RouteShadowDecisionsTotal, after.RouteShadowDecisionsTotal)
	assert.Equal(t, before.RouteShadowEventDroppedTotal, after.RouteShadowEventDroppedTotal)
}

func TestCapabilityRefreshPublishesActiveIndexAndHonorsAbilityChanges(t *testing.T) {
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDB := common.MainDatabaseType()
	originalLogDatabase := common.LogDatabaseType()
	previousIndex, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		routeCapabilityIndex.Store(previousIndex)
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDB, originalLogDatabase)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{}))
	channel := &model.Channel{Id: 601, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "openai/gpt-5", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "default", Model: "openai/gpt-5", Enabled: true}).Error)

	require.NoError(t, InitRouteCapabilityIndex(context.Background()))
	decision := SelectRouteShadow(RouteShadowRequest{RequestModel: "openai/gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true})
	assert.Equal(t, channel.Id, decision.ShadowPreferredID)

	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", false).Error)
	summary, err := RefreshStaleChannelCapabilities(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Refreshed)
	decision = SelectRouteShadow(RouteShadowRequest{RequestModel: "openai/gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true})
	assert.Equal(t, 0, decision.ShadowPreferredID)
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterAbilityDisabled])
}
