package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The preview and the request path must reach the same verdict about protocol
// capability. Before RequestRequirements existed the preview checked only the
// endpoint type, so it advertised channels that a real request rejected for an
// unsupported protocol path.
func TestPreviewAndRequestPathAgreeOnNativeResponsesSupport(t *testing.T) {
	requirements := NewRequestRequirements("gpt-5", "/v1/responses", true, relaycommon.CompactAttemptNone)

	for _, test := range []struct {
		name        string
		channelType int
		wantReason  string
	}{
		{name: "openai serves native responses", channelType: constant.ChannelTypeOpenAI, wantReason: ""},
		{name: "gemini cannot serve native responses", channelType: constant.ChannelTypeGemini, wantReason: RouteFilterPathUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Id: 91, Type: test.channelType, Status: common.ChannelStatusEnabled}
			assert.Equal(t, test.wantReason, requirements.ChannelMeetsRequirements(channel, nil))
		})
	}
}

func TestRequestRequirementFeaturesFromJSONCapturesSemanticRequirements(t *testing.T) {
	features := RequestRequirementFeaturesFromJSON([]byte(`{
		"previous_response_id":"resp_123",
		"input":[{"role":"user","content":[
			{"type":"input_text","text":"describe"},
			{"type":"input_image","image_url":"data:image/png;base64,AA=="}
		]}],
		"tools":[{"type":"function","name":"lookup"},{"type":"web_search_preview"}],
		"text":{"format":{"type":"json_schema","name":"answer"}},
		"reasoning":{"effort":"high"}
	}`))

	assert.Equal(t, []string{"image", "text"}, features.InputModalities)
	assert.Equal(t, []string{"function", "web_search_preview"}, features.ToolTypes)
	assert.True(t, features.StructuredOutput)
	assert.True(t, features.Reasoning)
	assert.True(t, features.Continuation)
}

func TestRequestRequirementFeaturesFromJSONUnderstandsGeminiShapes(t *testing.T) {
	features := RequestRequirementFeaturesFromJSON([]byte(`{
		"contents":[{"parts":[{"text":"hello"},{"inlineData":{"mimeType":"audio/wav","data":"AA=="}}]}],
		"tools":[{"functionDeclarations":[{"name":"lookup"}]},{"codeExecution":{}}],
		"generationConfig":{"responseMimeType":"application/json","thinkingConfig":{"thinkingBudget":32}}
	}`))

	assert.Equal(t, []string{"audio", "text"}, features.InputModalities)
	assert.Equal(t, []string{"code_execution", "function"}, features.ToolTypes)
	assert.True(t, features.StructuredOutput)
	assert.True(t, features.Reasoning)
	assert.False(t, features.Continuation)
}

func TestRequestRequirementFeaturesFromJSONLeavesInvalidBodyToNormalValidation(t *testing.T) {
	assert.Equal(t, RequestRequirementFeatures{}, RequestRequirementFeaturesFromJSON([]byte(`{"input":`)))
}

func TestRequestRequirementFeaturesFromJSONDoesNotTreatToolSchemaFormatAsOutputFormat(t *testing.T) {
	features := RequestRequirementFeaturesFromJSON([]byte(`{
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"format":{"type":"string"}}}}}]
	}`))

	assert.Equal(t, []string{"text"}, features.InputModalities)
	assert.Equal(t, []string{"function"}, features.ToolTypes)
	assert.False(t, features.StructuredOutput)
}

func TestRequestRequirementFeaturesFromJSONCapturesRootProtocolState(t *testing.T) {
	features := RequestRequirementFeaturesFromJSON([]byte(`{
		"conversation":{"id":"conv_123"},
		"container":"container_123",
		"output_format":{"type":"json_schema"}
	}`))

	assert.True(t, features.Continuation)
	assert.True(t, features.StructuredOutput)
}

// A Compact request looks up its exact variant before the base model, while
// every other protocol resolves exactly one model. Sharing this ordering keeps
// candidate discovery identical across callers.
func TestRequestRequirementsCandidateLookupModels(t *testing.T) {
	identity := func(name string) string { return name }

	for _, test := range []struct {
		name  string
		stage relaycommon.CompactAttemptStage
		path  string
		want  []string
	}{
		{
			name: "plain chat resolves one model", stage: relaycommon.CompactAttemptNone,
			path: "/v1/chat/completions", want: []string{"gpt-5"},
		},
		{
			name: "compact exact tries the variant first", stage: relaycommon.CompactAttemptExact,
			path: "/v1/responses/compact", want: []string{"gpt-5-openai-compact", "gpt-5"},
		},
		{
			name: "compact base resolves the base model", stage: relaycommon.CompactAttemptBase,
			path: "/v1/responses/compact", want: []string{"gpt-5"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requirements := NewRequestRequirements("gpt-5", test.path, false, test.stage)
			assert.Equal(t, test.want, requirements.CandidateLookupModels(identity))
		})
	}
}

// The preview derives the Compact stage from its own path exactly as the
// distributor does, so a Compact preview no longer reports channels that a real
// Compact request would reject.
func TestPreviewDerivesCompactStageLikeTheRequestPath(t *testing.T) {
	for _, test := range []struct {
		name  string
		path  string
		model string
		want  relaycommon.CompactAttemptStage
	}{
		{name: "chat is not compact", path: "/v1/chat/completions", model: "gpt-5", want: relaycommon.CompactAttemptNone},
		{name: "responses is not compact", path: "/v1/responses", model: "gpt-5", want: relaycommon.CompactAttemptNone},
		{name: "compact base model uses exact stage", path: "/v1/responses/compact", model: "gpt-5", want: relaycommon.CompactAttemptExact},
		{name: "non compact base model falls back to base", path: "/v1/responses/compact", model: "claude-opus-4-8", want: relaycommon.CompactAttemptBase},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, CompactStageForRequest(test.path, test.model))
		})
	}
}

// A preview must apply the same Compact capability rule as the request path.
// The capability here advertises the Compact endpoint, so the endpoint check
// passes and only the shared requirements can reject the channel — which is
// exactly the divergence this type removes.
func TestPreviewRejectsChannelThatCannotServeCompact(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID,
		[]string{string(constant.EndpointTypeOpenAI), string(constant.EndpointTypeOpenAIResponseCompact)},
		model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	// A plain chat path is served, proving the fixture is otherwise eligible.
	preview, err := PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Empty(t, preview.FilterReasonCounts)
	assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)

	// The channel's provider type cannot serve Compact, so a Compact preview
	// must report it instead of advertising the channel as usable.
	preview, err = PreviewUserRouteProfile(context.Background(), userID, created.Profile.ID, RouteProfilePreviewInput{
		Model: "gpt-test", Path: "/v1/responses/compact",
	})
	require.NoError(t, err)
	assert.Empty(t, preview.CandidateChannelIDs)
	assert.Equal(t, 1, preview.FilterReasonCounts[RouteFilterPathUnsupported])
}

func TestPreviewInputCarriesTheSameSemanticFeatureShape(t *testing.T) {
	input := RouteProfilePreviewInput{
		Model:            "gpt-test",
		Path:             "/v1/responses",
		InputModalities:  []string{"image", "text"},
		ToolTypes:        []string{"function"},
		StructuredOutput: true,
		Reasoning:        true,
		Continuation:     true,
	}
	requirements := NewRequestRequirementsWithFeatures(
		input.Model,
		input.Path,
		false,
		CompactStageForRequest(input.Path, input.Model),
		RequestRequirementFeatures{
			InputModalities:  input.InputModalities,
			ToolTypes:        input.ToolTypes,
			StructuredOutput: input.StructuredOutput,
			Reasoning:        input.Reasoning,
			Continuation:     input.Continuation,
		},
	)

	assert.Equal(t, input.InputModalities, requirements.InputModalities)
	assert.Equal(t, input.ToolTypes, requirements.ToolTypes)
	assert.True(t, requirements.StructuredOutput)
	assert.True(t, requirements.Reasoning)
	assert.True(t, requirements.Continuation)
}

func TestRequestRequirementsCarryStreamMode(t *testing.T) {
	requirements := NewRequestRequirementsWithStream(
		"gpt-test",
		"/v1/chat/completions",
		false,
		relaycommon.CompactAttemptNone,
		true,
		RequestRequirementFeatures{},
	)

	assert.True(t, requirements.StreamMode)
}

func TestRequestRequirementsPreferNativeProtocolCandidates(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		channelType int
		wantTier    int
	}{
		{name: "OpenAI chat native", path: "/v1/chat/completions", channelType: constant.ChannelTypeOpenAI, wantTier: 0},
		{name: "OpenAI chat through Claude conversion", path: "/v1/chat/completions", channelType: constant.ChannelTypeAnthropic, wantTier: 1},
		{name: "Claude messages native", path: "/v1/messages", channelType: constant.ChannelTypeAnthropic, wantTier: 0},
		{name: "Claude messages through OpenAI conversion", path: "/v1/messages", channelType: constant.ChannelTypeOpenAI, wantTier: 1},
		{name: "Gemini native", path: "/v1beta/models/gemini-2.5-flash:generateContent", channelType: constant.ChannelTypeGemini, wantTier: 0},
		{name: "Responses native", path: "/v1/responses", channelType: constant.ChannelTypeOpenAI, wantTier: 0},
		{name: "Responses through Gemini conversion", path: "/v1/responses", channelType: constant.ChannelTypeGemini, wantTier: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requirements := NewRequestRequirements("gpt-test", test.path, false, relaycommon.CompactAttemptNone)
			assert.Equal(t, test.wantTier, requirements.CandidateCompatibilityTier(&model.Channel{Type: test.channelType}))
		})
	}
}
