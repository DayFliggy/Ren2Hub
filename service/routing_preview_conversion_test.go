package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoutePreviewConversionPolicyAndExecutableCandidates(t *testing.T) {
	for _, test := range []struct {
		name        string
		channelType int
		policy      string
		path        string
		request     string
		target      types.RelayFormat
		diagnostic  string
		eligible    bool
	}{
		{"native", constant.ChannelTypeOpenAI, "", "/v1/chat/completions", `{"model":"gpt-test","stream":true}`, types.RelayFormatOpenAI, "", true},
		{"portable function", constant.ChannelTypeAnthropic, "safe", "/v1/chat/completions", `{"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, types.RelayFormatClaude, "", true},
		{"safe rejects loss", constant.ChannelTypeGemini, "safe", "/v1/chat/completions", `{"parallel_tool_calls":false}`, types.RelayFormatGemini, "serial_tools_not_portable", false},
		{"allow reports loss", constant.ChannelTypeGemini, "allow", "/v1/chat/completions", `{"parallel_tool_calls":false}`, types.RelayFormatGemini, "serial_tools_not_portable", true},
		{"strict rejects warning", constant.ChannelTypeAnthropic, "strict", "/v1/chat/completions", `{"user":"preview-user"}`, types.RelayFormatClaude, "metadata_not_portable", false},
		{"strict rejects unknown converted field", constant.ChannelTypeAnthropic, "strict", "/v1/chat/completions", `{"vendor_flag":false}`, types.RelayFormatClaude, "unknown_request_field", false},
		{"strict rejects unknown native field", constant.ChannelTypeOpenAI, "strict", "/v1/chat/completions", `{"vendor_limit":0}`, types.RelayFormatOpenAI, "unknown_request_field", false},
		{"safe reports unknown field", constant.ChannelTypeAnthropic, "safe", "/v1/chat/completions", `{"vendor_flag":false}`, types.RelayFormatClaude, "unknown_request_field", true},
		{"allow cannot move state", constant.ChannelTypeGemini, "allow", "/v1/responses", `{"previous_response_id":"response-preview"}`, types.RelayFormatGemini, "native_state_required", false},
		{"native prompt domain", constant.ChannelTypeOpenAI, "safe", "/v1/responses", `{"prompt":{"id":"pmpt_local"}}`, types.RelayFormatOpenAIResponses, "", true},
		{"allow cannot change prompt domain", constant.ChannelTypeOpenRouter, "allow", "/v1/responses", `{"prompt":{"id":"pmpt_local"}}`, types.RelayFormatOpenAIResponses, "reference_domain_unsupported", false},
		{"unsupported adaptor", constant.ChannelTypeJina, "safe", "/v1/chat/completions", `{}`, "", RoutePreviewFilterConversion, false},
		{"ollama function", constant.ChannelTypeOllama, "safe", "/v1/chat/completions", `{"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, types.RelayFormatOpenAI, "", true},
		{"ollama required tool", constant.ChannelTypeOllama, "safe", "/v1/chat/completions", `{"tool_choice":"required"}`, types.RelayFormatOpenAI, "projection_feature_unavailable", false},
		{"ollama allow cannot force tool", constant.ChannelTypeOllama, "allow", "/v1/chat/completions", `{"tool_choice":"required"}`, types.RelayFormatOpenAI, "projection_feature_unavailable", false},
		{"ollama allow cannot serialize tools", constant.ChannelTypeOllama, "allow", "/v1/chat/completions", `{"parallel_tool_calls":false}`, types.RelayFormatOpenAI, "projection_feature_unavailable", false},
		{"ollama strict tool schema", constant.ChannelTypeOllama, "allow", "/v1/chat/completions", `{"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":true}}]}`, types.RelayFormatOpenAI, "projection_feature_unavailable", false},
		{"ollama developer role", constant.ChannelTypeOllama, "allow", "/v1/chat/completions", `{"messages":[{"role":"developer","content":"instruction"},{"role":"user","content":"hello"}]}`, types.RelayFormatOpenAI, "projection_role_dropped", false},
		{"ollama sampling loss", constant.ChannelTypeOllama, "safe", "/v1/chat/completions", `{"logprobs":true}`, types.RelayFormatOpenAI, "projection_field_dropped", true},
		{"ollama strict sampling loss", constant.ChannelTypeOllama, "strict", "/v1/chat/completions", `{"logprobs":true}`, types.RelayFormatOpenAI, "projection_field_dropped", false},
		{"ollama native Claude forced tool", constant.ChannelTypeOllama, "safe", "/v1/messages", `{"max_tokens":32,"tool_choice":{"type":"any"}}`, types.RelayFormatClaude, "", true},
		{"ollama Gemini forced tool", constant.ChannelTypeOllama, "safe", "/v1beta/models/gpt-test:generateContent", `{"toolConfig":{"functionCallingConfig":{"mode":"ANY"}}}`, types.RelayFormatOpenAI, "projection_feature_unavailable", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupRouteProfileTest(t)
			userID, tokenID, channelID := seedRouteProfileFixture(t, db)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("type", test.channelType).Error)
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("conversion_policy", test.policy).Error)
			publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI), string(constant.EndpointTypeOpenAIResponse), string(constant.EndpointTypeAnthropic), string(constant.EndpointTypeGemini)}, model.RouteCapabilityStateEligible)
			profile, err := CreateUserRouteProfile(RouteProfileInput{
				UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
				Groups: []RouteGroupInput{{Name: "primary", Enabled: true, Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}}}},
			})
			require.NoError(t, err)
			// A caller cannot override the token policy through preview input.
			var input RouteProfilePreviewInput
			require.NoError(t, common.Unmarshal([]byte(`{"conversion_policy":"allow"}`), &input))
			input.Model, input.Path, input.Request = "gpt-test", test.path, json.RawMessage(test.request)
			preview, err := PreviewUserRouteProfile(context.Background(), userID, profile.Profile.ID, input)
			require.NoError(t, err)
			require.Len(t, preview.Entries, 1)
			entry := preview.Entries[0]
			require.NotNil(t, entry.ConversionPlan)
			assert.Equal(t, test.target, entry.ConversionPlan.To)
			if test.policy == "" {
				assert.Equal(t, types.ConversionLossPolicySafe, preview.ConversionPolicy)
			} else {
				assert.Equal(t, types.ConversionLossPolicy(test.policy), preview.ConversionPolicy)
			}
			assert.Equal(t, preview.ConversionPolicy, entry.ConversionPlan.Policy)
			assert.False(t, preview.LiveSelection)
			if test.eligible {
				assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
				assert.Empty(t, entry.FilterReason)
			} else {
				assert.Empty(t, preview.CandidateChannelIDs)
				assert.Equal(t, RoutePreviewFilterConversion, entry.FilterReason)
				assert.Equal(t, 1, preview.FilterReasonCounts[RoutePreviewFilterConversion])
			}
			if test.diagnostic != "" {
				require.NotEmpty(t, entry.Diagnostics)
				assert.Equal(t, test.diagnostic, entry.Diagnostics[0].Code)
			}
		})
	}
}

func TestRoutePreviewRejectsDroppedChannelInstructionsEvenWithAllow(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"type": constant.ChannelTypeCoze, "setting": `{"system_prompt":"private-channel-instruction"}`,
	}).Error)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("conversion_policy", "allow").Error)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	profile, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "primary", Enabled: true, Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}}}},
	})
	require.NoError(t, err)
	preview, err := PreviewUserRouteProfile(context.Background(), userID, profile.Profile.ID, RouteProfilePreviewInput{Model: "gpt-test", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	require.Len(t, preview.Entries, 1)
	assert.Empty(t, preview.CandidateChannelIDs)
	require.Len(t, preview.Entries[0].Diagnostics, 1)
	assert.Equal(t, "channel_system_prompt_unsupported", preview.Entries[0].Diagnostics[0].Code)
	encoded, err := common.Marshal(preview)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-channel-instruction")
}

func TestRoutePreviewRequestValidationAndReadOnlyBoundary(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("type", constant.ChannelTypeGemini).Error)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	profile, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "primary", Enabled: true, Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}}}},
	})
	require.NoError(t, err)
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(upstream.Close)
	input := RouteProfilePreviewInput{Model: "gpt-test", Path: "/v1/chat/completions"}
	input.Request, err = common.Marshal(map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": upstream.URL + "/image.png"}}}}},
	})
	require.NoError(t, err)
	var beforeUser model.User
	var beforeToken model.Token
	var beforeChannel model.Channel
	require.NoError(t, db.First(&beforeUser, userID).Error)
	require.NoError(t, db.First(&beforeToken, tokenID).Error)
	require.NoError(t, db.First(&beforeChannel, channelID).Error)
	preview, err := PreviewUserRouteProfile(context.Background(), userID, profile.Profile.ID, input)
	require.NoError(t, err)
	assert.Equal(t, []int{channelID}, preview.CandidateChannelIDs)
	assert.Contains(t, preview.RequestRequirements.Features, "multimodal")
	assert.Zero(t, requests.Load(), "planning must not fetch media or call upstream")
	var afterUser model.User
	var afterToken model.Token
	var afterChannel model.Channel
	require.NoError(t, db.First(&afterUser, userID).Error)
	require.NoError(t, db.First(&afterToken, tokenID).Error)
	require.NoError(t, db.First(&afterChannel, channelID).Error)
	assert.Equal(t, beforeUser, afterUser)
	assert.Equal(t, beforeToken, afterToken)
	assert.Equal(t, beforeChannel, afterChannel)
	var healthCount int64
	require.NoError(t, db.Model(&model.ChannelHealth{}).Count(&healthCount).Error)
	assert.Zero(t, healthCount, "preview must not claim health probes")
	_, err = PreviewUserRouteProfile(context.Background(), userID+1, profile.Profile.ID, input)
	assert.ErrorIs(t, err, ErrRouteProfileNotFound)
	for _, sample := range []string{`null`, `[]`, `{"model":"another-model"}`, `{"stream":"true"}`} {
		input.Request = json.RawMessage(sample)
		_, err := PreviewUserRouteProfile(context.Background(), userID, profile.Profile.ID, input)
		assert.ErrorIs(t, err, ErrRouteProfileValidation, sample)
	}
}
