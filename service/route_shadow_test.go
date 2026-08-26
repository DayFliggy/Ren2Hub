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
					Confidence: 1, State: model.RouteCapabilityStateEligible, SnapshotVersion: 1,
					CatalogVersion: "catalog-1", EndpointTypes: string(endpointJSON),
				},
				ChannelStatus: common.ChannelStatusEnabled, Priority: 20, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
			},
			{
				Capability: model.ChannelModelCapability{
					ChannelID: 2, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai",
					Confidence: 1, State: model.RouteCapabilityStateEligible, SnapshotVersion: 1,
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
		PriceEligible: true, PriceEligibilityKnown: true,
		SecurityAllowed: true, SecurityEligibilityKnown: true,
		Legacy: LegacySelectionTrace{SelectedChannelID: 1, PriorityLayers: map[int64][]int{20: {1}}},
	})

	assert.Equal(t, 1, decision.ShadowPreferredID)
	assert.Equal(t, ShadowReasonSameChannel, decision.DifferenceReasons[0])
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterGroupForbidden])
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterUnknownCapability])
	assert.True(t, decision.HasUnauthorized)
	assert.True(t, decision.HasUnknown)

	deferred := SelectRouteShadow(RouteShadowRequest{
		RequestModel: "gpt-5", UserGroup: "default", EndpointType: "openai",
	})
	assert.Equal(t, 1, deferred.ShadowPreferredID)
	assert.True(t, deferred.RuntimeRecheckRequired)
	assert.ElementsMatch(t, []string{"price_qualification", "security_policy"}, deferred.RuntimeRecheckReasons)
	assert.False(t, deferred.PriceEligibilityKnown)
	assert.False(t, deferred.SecurityEligibilityKnown)

	priceDenied := SelectRouteShadow(RouteShadowRequest{
		RequestModel: "gpt-5", UserGroup: "default", EndpointType: "openai",
		PriceEligibilityKnown: true, SecurityEligibilityKnown: true, SecurityAllowed: true,
	})
	assert.Zero(t, priceDenied.ShadowPreferredID)
	assert.Equal(t, 1, priceDenied.FilterReasonCounts[ShadowFilterPriceForbidden])

	securityDenied := SelectRouteShadow(RouteShadowRequest{
		RequestModel: "gpt-5", UserGroup: "default", EndpointType: "openai",
		PriceEligibilityKnown: true, PriceEligible: true, SecurityEligibilityKnown: true,
	})
	assert.Zero(t, securityDenied.ShadowPreferredID)
	assert.Equal(t, 1, securityDenied.FilterReasonCounts[ShadowFilterSecurityForbidden])
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
			{Capability: model.ChannelModelCapability{ChannelID: 9, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Confidence: 1, State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON)}, ChannelStatus: common.ChannelStatusEnabled, Priority: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI},
			{Capability: model.ChannelModelCapability{ChannelID: 8, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Confidence: 1, State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON)}, ChannelStatus: common.ChannelStatusEnabled, Priority: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI},
			{Capability: model.ChannelModelCapability{ChannelID: 7, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Confidence: 1, State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON)}, ChannelStatus: common.ChannelStatusEnabled, Priority: 20, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeAdvancedCustom, Advanced: advanced},
		},
	}})

	decision := SelectRouteShadow(RouteShadowRequest{RequestModel: "gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true, RequestPath: "/v1/unsupported"})
	assert.Equal(t, 8, decision.ShadowPreferredID)
	assert.Equal(t, 1, decision.FilterReasonCounts[ShadowFilterPathUnsupported])

	decision = SelectRouteShadow(RouteShadowRequest{RequestModel: "gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true, RequestPath: "/v1/chat/completions"})
	assert.Equal(t, 7, decision.ShadowPreferredID)
}

func TestSelectRouteShadowRejectsStaleOrMissingActiveSnapshot(t *testing.T) {
	oldIndex, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	t.Cleanup(func() { routeCapabilityIndex.Store(oldIndex) })
	routeCapabilityIndex.Store(&capabilityIndex{ByRequestModel: map[string][]indexedCapability{
		"gpt-5": {{
			Capability: model.ChannelModelCapability{
				ChannelID: 51, RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai",
				Confidence: 1, State: model.RouteCapabilityStateEligible, SnapshotVersion: 2,
			},
			ChannelStatus: common.ChannelStatusEnabled, Priority: 10,
			AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
		}},
	}})

	stale := SelectRouteShadow(RouteShadowRequest{
		RequestModel: "gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true,
		ActiveSnapshotVersions: map[int]int64{51: 1},
	})
	assert.Zero(t, stale.ShadowPreferredID)
	assert.Equal(t, 1, stale.FilterReasonCounts[ShadowFilterSnapshotStale])

	missing := SelectRouteShadow(RouteShadowRequest{
		RequestModel: "gpt-5", UserGroup: "default", PriceEligible: true, SecurityAllowed: true,
		ActiveSnapshotVersions: map[int]int64{},
	})
	assert.Zero(t, missing.ShadowPreferredID)
	assert.Equal(t, 1, missing.FilterReasonCounts[ShadowFilterSnapshotUnavailable])
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
				Confidence: 1, State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpointJSON),
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

func TestReplayRouteShadowDecisionReadsHistoricalSnapshotOnly(t *testing.T) {
	originalDB := model.DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{}))
	endpointJSON, err := common.Marshal([]string{"openai"})
	require.NoError(t, err)
	groupsJSON, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), 77, model.ChannelCapabilitySnapshotFence{}, "replay-hash", "catalog-replay", []model.ChannelModelCapability{{
		RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical", Confidence: 0.99,
		AbilityGroups: string(groupsJSON), EndpointTypes: string(endpointJSON), ChannelStatus: common.ChannelStatusEnabled,
		Priority: 20, Weight: 10, ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))

	decision := RouteShadowDecision{
		Event: "route_shadow_decision", RouteSource: ShadowRouteSource, QualificationVersion: RouteShadowQualificationVersion,
		RequestID: "replay-request", UserID: 7, TokenID: 8,
		RequestModel: "gpt-5", NormalizedRequestModel: "gpt-5", RequestPath: "/v1/chat/completions",
		UserGroup: "default", EndpointType: string(constant.EndpointTypeOpenAI), SnapshotVersion: 1,
		PriceEligible: true, PriceEligibilityKnown: true, SecurityAllowed: true, SecurityEligibilityKnown: true,
		ShadowCandidates: []RouteShadowCandidate{{ChannelID: 77, RequestModel: "gpt-5", SnapshotVersion: 1, Priority: 20}},
		LegacyTrace:      LegacySelectionTrace{CandidateIDs: []int{77}, SelectedChannelID: 77, SelectedGroup: "default"},
	}
	data, err := common.Marshal(decision)
	require.NoError(t, err)

	replayed, err := ReplayRouteShadowDecision(context.Background(), data)
	require.NoError(t, err)
	assert.Equal(t, 77, replayed.ShadowPreferredID)
	assert.Equal(t, 77, replayed.LegacyChannelID)
	assert.Equal(t, int64(1), replayed.SnapshotVersion)
}

func TestReplayRouteShadowDecisionRejectsLegacyCapabilityProjection(t *testing.T) {
	originalDB := model.DB
	originalMainDB := common.MainDatabaseType()
	originalLogDB := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = originalDB
		common.SetDatabaseTypes(originalMainDB, originalLogDB)
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelModelCapability{}, &model.ChannelCapabilitySnapshot{}))
	endpointJSON, err := common.Marshal([]string{"openai"})
	require.NoError(t, err)
	groupsJSON, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), 78, model.ChannelCapabilitySnapshotFence{}, "legacy-projection", "catalog-replay", []model.ChannelModelCapability{{
		RequestModel: "gpt-5", ActualModel: "gpt-5", LabSlug: "openai", Source: "canonical",
		AbilityGroups: string(groupsJSON), EndpointTypes: string(endpointJSON), ChannelStatus: common.ChannelStatusEnabled,
		Priority: 20, ChannelType: constant.ChannelTypeOpenAI, State: model.RouteCapabilityStateEligible,
	}}))
	data, err := common.Marshal(RouteShadowDecision{
		Event: "route_shadow_decision", RouteSource: ShadowRouteSource, QualificationVersion: RouteShadowQualificationVersion,
		RequestID: "legacy-projection-request", RequestModel: "gpt-5", NormalizedRequestModel: "gpt-5",
		RequestPath: "/v1/chat/completions", UserGroup: "default", EndpointType: string(constant.EndpointTypeOpenAI), SnapshotVersion: 1,
		PriceEligible: true, PriceEligibilityKnown: true, SecurityAllowed: true, SecurityEligibilityKnown: true,
		ShadowCandidates: []RouteShadowCandidate{{ChannelID: 78, RequestModel: "gpt-5", SnapshotVersion: 1}},
		LegacyTrace:      LegacySelectionTrace{SelectedChannelID: 78},
	})
	require.NoError(t, err)
	_, err = ReplayRouteShadowDecision(context.Background(), data)
	assert.ErrorIs(t, err, ErrRouteShadowReplayUnsupported)
}

func TestReplayRouteShadowDecisionRejectsIncompleteEvent(t *testing.T) {
	data, err := common.Marshal(RouteShadowDecision{RequestID: "missing-path", RequestModel: "gpt-5", SnapshotVersion: 1})
	require.NoError(t, err)
	_, err = ReplayRouteShadowDecision(context.Background(), data)
	assert.ErrorIs(t, err, ErrRouteShadowReplayInvalid)
}

func TestReplayRouteShadowDecisionRejectsPreferredCandidateSnapshotMismatch(t *testing.T) {
	data, err := common.Marshal(RouteShadowDecision{
		Event: "route_shadow_decision", RouteSource: ShadowRouteSource, QualificationVersion: RouteShadowQualificationVersion,
		RequestID: "snapshot-mismatch", RequestModel: "gpt-5", RequestPath: "/v1/chat/completions",
		UserGroup: "default", SnapshotVersion: 2, ShadowPreferredID: 77,
		ShadowCandidates: []RouteShadowCandidate{{ChannelID: 77, RequestModel: "gpt-5", SnapshotVersion: 1}},
		LegacyTrace:      LegacySelectionTrace{CandidateIDs: []int{77}},
	})
	require.NoError(t, err)

	_, err = ReplayRouteShadowDecision(context.Background(), data)
	assert.ErrorIs(t, err, ErrRouteShadowReplayInvalid)
}

func TestReplayRouteShadowDecisionRejectsUntrustedEnvelopeFields(t *testing.T) {
	data := []byte(`{"event":"route_shadow_decision","route_source":"auto_lab","qualification_version":2,"request_id":"replay-request","request_model":"gpt-5","request_path":"/v1/chat/completions","user_group":"default","snapshot_version":1,"shadow_candidates":[{"channel_id":77,"snapshot_version":1}],"legacy_trace":{},"request_body":"sensitive"}`)
	_, err := ReplayRouteShadowDecision(context.Background(), data)
	assert.ErrorIs(t, err, ErrRouteShadowReplayInvalid)

	data = []byte(`{"event":"other_event","route_source":"auto_lab","qualification_version":2,"request_id":"replay-request","request_model":"gpt-5","request_path":"/v1/chat/completions","user_group":"default","snapshot_version":1,"shadow_candidates":[{"channel_id":77,"snapshot_version":1}],"legacy_trace":{}}`)
	_, err = ReplayRouteShadowDecision(context.Background(), data)
	assert.ErrorIs(t, err, ErrRouteShadowReplayInvalid)
}

func TestReplayRouteShadowDecisionRejectsQualificationVersionsWithoutInference(t *testing.T) {
	data, err := common.Marshal(RouteShadowDecision{
		Event: "route_shadow_decision", RouteSource: ShadowRouteSource, QualificationVersion: 1,
		RequestID: "old-qualification", RequestModel: "gpt-5", RequestPath: "/v1/chat/completions",
		UserGroup: "default", SnapshotVersion: 1,
		ShadowCandidates: []RouteShadowCandidate{{ChannelID: 77, SnapshotVersion: 1}},
		LegacyTrace:      LegacySelectionTrace{CandidateIDs: []int{77}},
	})
	require.NoError(t, err)
	_, err = ReplayRouteShadowDecision(context.Background(), data)
	assert.ErrorIs(t, err, ErrRouteShadowReplayUnsupported)
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
