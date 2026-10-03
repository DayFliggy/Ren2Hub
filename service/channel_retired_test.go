package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRetiredChannelRejectsCachedCapabilitiesAndTaskBindings(t *testing.T) {
	channel := &model.Channel{Id: 57, Type: constant.ChannelTypeCodex, Models: "gpt-5-codex", Status: common.ChannelStatusEnabled, Setting: common.GetPointer(`{"responses_websocket_enabled":true}`)}
	assert.Empty(t, endpointTypesForModel(channel, modellab.ModelMatch{InputModel: "gpt-5-codex", RealModel: "gpt-5-codex"}))
	assert.False(t, TaskPluginChannelEligible(channel, ""))
	assert.False(t, TaskPluginChannelEligible(channel, "existing-plugin"))
	assert.False(t, ChannelSupportsCompactEndpoint(channel, "gpt-5-codex"))
	assert.False(t, ChannelSupportsNativeResponses(channel, "gpt-5-codex"))
	assert.False(t, ChannelSupportsResponsesWebSocket(channel, "gpt-5-codex"))
	// A formerly eligible snapshot must not resurrect the removed transport.
	result := filterRouteCapability(routeCapabilityFilterInput{
		Capability:      model.ChannelModelCapability{ChannelID: 57, ChannelType: constant.ChannelTypeOpenAI, LabSlug: "openai", Confidence: 1, Source: "channel_configuration", State: model.RouteCapabilityStateEligible, EndpointTypes: `["openai-response"]`, SnapshotVersion: 1},
		SnapshotVersion: 1, ChannelType: constant.ChannelTypeCodex, ChannelStatus: common.ChannelStatusEnabled,
		AbilityEnabled: true, Entitled: true, PriceEligibilityKnown: true, PriceEligible: true,
	})
	assert.Equal(t, RouteFilterUnsupported, result.Reason)
}

func TestActiveChannelRouteAdmissionAfterSubscriptionRetirement(t *testing.T) {
	for _, test := range []struct {
		name                   string
		channelType            int
		modelName, requestPath string
	}{
		{"OpenAI Codex", constant.ChannelTypeOpenAI, "gpt-5-codex", "/v1/responses"},
		{"Anthropic", constant.ChannelTypeAnthropic, "claude-sonnet-4", "/v1/messages"},
		{"Gemini", constant.ChannelTypeGemini, "gemini-2.5-pro", "/v1beta/models/gemini-2.5-pro:generateContent"},
		{"Sub2API", constant.ChannelTypeSub2API, "gpt-5-codex", "/v1/responses"},
		{"NewAPI", constant.ChannelTypeNewAPI, "gpt-5-codex", "/v1/responses"},
		{"Advanced Custom", constant.ChannelTypeAdvancedCustom, "gpt-4o", "/v1/chat/completions"},
		{"vLLM", constant.ChannelTypeVLLM, "gpt-4o", "/v1/chat/completions"},
		{"SGLang", constant.ChannelTypeSGLang, "gpt-4o", "/v1/chat/completions"},
		{"media task", constant.ChannelTypeSora, "sora-2", "/v1/videos"},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Id: 1, Type: test.channelType}
			var advanced *dto.AdvancedCustomConfig
			if constant.IsAdvancedCustomChannel(test.channelType) {
				advanced = &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: test.requestPath, UpstreamPath: test.requestPath, Converter: "none"}}}
				channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: advanced})
			}
			endpoints, err := common.Marshal(endpointTypesForModel(channel, modellab.ModelMatch{InputModel: test.modelName, RealModel: test.modelName}))
			require.NoError(t, err)
			input := routeCapabilityFilterInput{
				Capability:      model.ChannelModelCapability{ChannelID: 1, LabSlug: "provider", Confidence: 1, Source: "channel_configuration", State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpoints), SnapshotVersion: 1},
				SnapshotVersion: 1, ChannelType: test.channelType, ChannelStatus: common.ChannelStatusEnabled,
				AbilityEnabled: true, Entitled: true, PriceEligibilityKnown: true, PriceEligible: true, RequireSnapshot: true,
				RequestModel: test.modelName, RequestPath: test.requestPath, EndpointType: endpointTypeForRequestPath(test.requestPath), RequireEndpoint: true, Advanced: advanced,
			}
			assert.Empty(t, filterRouteCapability(input).Reason)
			input.PriceEligible = false
			assert.Equal(t, RouteFilterPriceForbidden, filterRouteCapability(input).Reason, "retirement must not bypass existing pricing qualification")
		})
	}
}

func TestRetiredChannelCannotAcquirePermitOrContinueState(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	channel := model.Channel{Id: 57, Type: constant.ChannelTypeCodex, Key: "historical-secret", BaseURL: common.GetPointer("https://retired.invalid"), Status: common.ChannelStatusEnabled, CapacityTotal: 10, ChannelRatio: "1"}
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&channel).Error)
	for _, probe := range []bool{false, true} {
		permit, _, err := AcquireGatewayChannelPermit(context.Background(), "retired-request", channel.Id, probe, nil)
		require.ErrorIs(t, err, constant.ErrRetiredChannelType)
		assert.Nil(t, permit)
	}
	assert.ErrorIs(t, RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{Context: context.Background(), UserID: 1, ChannelID: channel.Id, RequestModel: "gpt-5-codex", RequestPath: "/v1/responses"}), constant.ErrRetiredChannelType)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	CaptureGatewayChannelIdentity(c, &channel)
	assert.ErrorIs(t, ValidateGatewayChannelIdentity(c, channel.Id), constant.ErrRetiredChannelType)
	c.Set(GatewayStateContextKey, &model.GatewayStateBinding{ChannelID: channel.Id, EndpointID: channel.GatewayEndpointID(), TransportID: "historical-transport", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	key, _, err := ValidateGatewayStateOwner(c, channel.Id)
	assert.ErrorIs(t, err, model.ErrGatewayStateUnavailable)
	assert.Empty(t, key)
}
