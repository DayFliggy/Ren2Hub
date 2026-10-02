package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/modellab"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelManifestTest(t *testing.T) (*gorm.DB, int, int) {
	t.Helper()
	db := setupRouteProfileTest(t)
	require.NoError(t, db.AutoMigrate(&model.Model{}, &model.Option{}, &model.Vendor{}, &model.BillingPriceVersion{}))
	selfUse := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = false
	t.Cleanup(func() { operation_setting.SelfUseModeEnabled = selfUse })
	user := model.User{Username: "manifest-user", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "manifest-private-token", Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	return db, user.Id, token.Id
}

func seedManifestRoute(t *testing.T, db *gorm.DB, channelID int, name string, endpoint constant.EndpointType, state string) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Type: constant.ChannelTypeOpenAI, Models: name,
		Status: common.ChannelStatusEnabled, Key: "private-upstream-key",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Model: name, Enabled: true}).Error)
	encoded, err := common.Marshal([]constant.EndpointType{endpoint})
	require.NoError(t, err)
	var channel model.Channel
	var abilities []model.Ability
	require.NoError(t, db.First(&channel, channelID).Error)
	require.NoError(t, db.Where("channel_id = ?", channelID).Find(&abilities).Error)
	fingerprint, err := channelCapabilityFingerprint(&channel, abilities)
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, fingerprint, modellab.DefaultCatalog().Version, []model.ChannelModelCapability{{
		RequestModel: name, ActualModel: name, LabSlug: "custom", Source: "channel_configuration", Confidence: 1,
		EndpointTypes: string(encoded), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, State: state,
	}}))
}

func TestModelManifestFiltersRoutesPermissionsPricesAndPreservesUnknown(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	names := []string{"known", "unknown", "unpriced", "forbidden", "revoked", "disabled-channel", "disabled-card", "conflicting"}
	prices := make(map[string]string)
	for index, name := range names {
		state := model.RouteCapabilityStateEligible
		if name == "conflicting" {
			state = model.RouteCapabilityStateConflict
		}
		seedManifestRoute(t, db, index+1, name, constant.EndpointTypeOpenAI, state)
		if name != "unpriced" {
			prices[name] = "0"
		}
	}
	encodedPrices, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: string(encodedPrices)}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 6).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 5, Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
	disabled := model.Model{ModelName: "disabled-card", Status: 0}
	require.NoError(t, disabled.Insert())
	toolCall := false
	card := model.Model{ModelName: "known", Status: 1, Capabilities: &model.ModelCapabilities{ToolCall: &toolCall}}
	require.NoError(t, card.Insert())
	// A second, unauthorized channel must not add its endpoint to a visible model.
	seedManifestRoute(t, db, 20, "known", constant.EndpointTypeAnthropic, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 20, Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
	limits := make(map[string]bool)
	for _, name := range names {
		limits[name] = name != "forbidden"
	}
	manifest, err := BuildModelManifest(context.Background(), ModelManifestRequest{
		UserID: userID, TokenID: tokenID, ModelLimitEnabled: true, ModelLimit: limits,
	})
	require.NoError(t, err)
	require.Len(t, manifest.Models, 2)
	assert.Equal(t, "known", manifest.Models[0].ID)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, manifest.Models[0].SupportedEndpointTypes)
	require.NotNil(t, manifest.Models[0].Capabilities.ToolCall)
	assert.False(t, *manifest.Models[0].Capabilities.ToolCall)
	assert.Equal(t, "unknown", manifest.Models[1].ID)
	assert.Nil(t, manifest.Models[1].Capabilities)
	assert.True(t, manifest.Models[1].PriceConfigured, "an explicit zero price is configured")
	assert.NotEmpty(t, manifest.Models[1].PriceVersion)
	encoded, err := common.Marshal(manifest)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-upstream-key")
	assert.NotContains(t, string(encoded), "manifest-private-token")
	assert.NotContains(t, string(encoded), "channel_id")
	assert.NotContains(t, string(encoded), "ModelPrice")

	// Descriptive card writes cannot modify the editable price source.
	var savedPrice model.Option
	require.NoError(t, db.Where(map[string]any{"key": "ModelPrice"}).First(&savedPrice).Error)
	assert.Equal(t, string(encodedPrices), savedPrice.Value)
}

func TestModelManifestUsesActiveManualGroupAndFailsClosedOnDisabledProfile(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	seedManifestRoute(t, db, 41, "allowed", constant.EndpointTypeOpenAI, model.RouteCapabilityStateEligible)
	seedManifestRoute(t, db, 42, "outside", constant.EndpointTypeOpenAI, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"allowed":"0","outside":"0"}`}).Error)
	profile := model.UserRouteProfile{UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual, Status: model.RouteProfileStatusEnabled}
	require.NoError(t, db.Create(&profile).Error)
	group := model.UserRouteGroup{ProfileID: profile.ID, Enabled: true, Name: "active"}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&model.UserRouteEntry{GroupID: group.ID, ChannelID: 41, Source: model.RouteSourcePlatform, Enabled: true}).Error)
	require.NoError(t, db.Model(&profile).Update("active_group_id", group.ID).Error)
	input := ModelManifestRequest{UserID: userID, TokenID: tokenID}
	manifest, err := BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Equal(t, "allowed", manifest.Models[0].ID)
	require.NoError(t, db.Model(&profile).Update("status", model.RouteProfileStatusDisabled).Error)
	manifest, err = BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, manifest.Models)
}

func TestModelManifestDoesNotUseHistoricalOrMissingCapabilitySnapshots(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	seedManifestRoute(t, db, 51, "missing", constant.EndpointTypeOpenAI, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"missing":"0"}`}).Error)
	require.NoError(t, db.Model(&model.ChannelCapabilitySnapshot{}).Where("channel_id = ?", 51).Update("active_version", 2).Error)
	manifest, err := BuildModelManifest(context.Background(), ModelManifestRequest{UserID: userID, TokenID: tokenID})
	require.NoError(t, err)
	assert.Empty(t, manifest.Models, "historical capabilities cannot advertise an unavailable route")
}

func TestModelManifestTaskPricesRequireAnAuthorizedExecutablePlugin(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previousRegistry })
	const name = "shared-task-manifest-model"
	for index, key := range []string{"unpriced-provider", "priced-provider"} {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:%q,version:"1.0.0",author:{name:"Test"},models:[%q],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}}};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};`, key, key, name)
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
		channelID := 61 + index
		seedManifestRoute(t, db, channelID, name, constant.EndpointTypeTaskPlugin, model.RouteCapabilityStateEligible)
		settings, err := common.Marshal(map[string]string{"task_plugin_key": key})
		require.NoError(t, err)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": string(settings)}).Error)
	}
	priceJSON, err := common.Marshal(map[string]string{
		billing_setting.PluginBillingExprKey("priced-provider", name): `u("count") * 0.01`,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: billing_setting.PluginBillingExprOption, Value: string(priceJSON)}).Error)
	entitlement := model.UserChannelEntitlement{UserID: userID, ChannelID: 62, Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}
	require.NoError(t, db.Create(&entitlement).Error)
	input := ModelManifestRequest{UserID: userID, TokenID: tokenID}
	manifest, err := BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, manifest.Models, "a priced but unauthorized plugin cannot qualify the unpriced route")
	require.NoError(t, db.Delete(&entitlement).Error)
	manifest, err = BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Equal(t, name, manifest.Models[0].ID)
	assert.True(t, manifest.Models[0].PriceConfigured)
}

func TestModelManifestCompactSuffixIsNotAnEndpointPermission(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	seedManifestRoute(t, db, 71, "ordinary-model", constant.EndpointTypeOpenAIResponseCompact, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"ordinary-model":"0"}`}).Error)
	input := ModelManifestRequest{UserID: userID, TokenID: tokenID, ModelLimitEnabled: true,
		ModelLimit: map[string]bool{"ordinary-model-openai-compact": true}}
	manifest, err := BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, manifest.Models)
	input.ModelLimit = map[string]bool{"ordinary-model": true}
	manifest, err = BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponseCompact}, manifest.Models[0].SupportedEndpointTypes)
}

func TestModelManifestRejectsForeignTokenAndRechecksCurrentModelLimits(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	seedManifestRoute(t, db, 81, "restricted-model", constant.EndpointTypeOpenAI, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"restricted-model":"0"}`}).Error)
	manifest, err := BuildModelManifest(context.Background(), ModelManifestRequest{UserID: userID + 1, TokenID: tokenID})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	assert.Empty(t, manifest.Models)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"model_limits_enabled": true, "model_limits": "another-model",
	}).Error)
	manifest, err = BuildModelManifest(context.Background(), ModelManifestRequest{
		UserID: userID, TokenID: tokenID, ModelLimitEnabled: true,
		ModelLimit: map[string]bool{"restricted-model": true},
	})
	require.NoError(t, err)
	assert.Empty(t, manifest.Models, "a stale caller projection cannot broaden the current token's model permissions")
}

func TestModelManifestInitializesAuthorizedNewChannelsAndRefreshesConfiguration(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	channel := model.Channel{Id: 91, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled, Models: "fresh-model"}
	channel.SetOtherSettings(relaydto.ChannelOtherSettings{AdvancedCustom: &relaydto.AdvancedCustomConfig{
		Routes: []relaydto.AdvancedCustomRoute{{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions"}},
	}})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: "fresh-model", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 92, Type: constant.ChannelTypeOpenAI, Models: "private-model", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 92, Model: "private-model", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 92, Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"fresh-model":"0","private-model":"0"}`}).Error)
	var count int64
	require.NoError(t, db.Model(&model.ChannelCapabilitySnapshot{}).Count(&count).Error)
	assert.Zero(t, count, "normal channel creation starts without prebuilt snapshots")
	input := ModelManifestRequest{UserID: userID, TokenID: tokenID}
	manifest, err := BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Equal(t, "fresh-model", manifest.Models[0].ID)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, manifest.Models[0].SupportedEndpointTypes)
	var initial model.ChannelCapabilitySnapshot
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&initial).Error)
	assert.Positive(t, initial.ActiveVersion)
	require.NoError(t, db.Model(&model.ChannelCapabilitySnapshot{}).Where("channel_id = ?", 92).Count(&count).Error)
	assert.Zero(t, count, "model discovery must not initialize a revoked channel")
	_, err = BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	var unchanged model.ChannelCapabilitySnapshot
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&unchanged).Error)
	assert.Equal(t, initial.ActiveVersion, unchanged.ActiveVersion, "unchanged fingerprints reuse the active snapshot")

	channel.SetOtherSettings(relaydto.ChannelOtherSettings{AdvancedCustom: &relaydto.AdvancedCustomConfig{
		Routes: []relaydto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses"}},
	}})
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("settings", channel.OtherSettings).Error)
	manifest, err = BuildModelManifest(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponse}, manifest.Models[0].SupportedEndpointTypes)
	var changed model.ChannelCapabilitySnapshot
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&changed).Error)
	assert.Greater(t, changed.ActiveVersion, initial.ActiveVersion)
	assert.NotEqual(t, initial.SourceHash, changed.SourceHash)
}
