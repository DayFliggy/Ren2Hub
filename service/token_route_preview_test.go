package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenPreviewLegacyAutoOwnershipAndNoReservations(t *testing.T) {
	db := setupRouteProfileTest(t)
	user, token, channel := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channel, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	input := RouteProfilePreviewInput{Model: "gpt-test", Path: "/v1/chat/completions"}
	before := ReadGatewayAdmissionRuntime(ChannelAdmissionKey(channel))
	preview, err := PreviewUserTokenRoute(context.Background(), user, token, input)
	require.NoError(t, err)
	assert.Equal(t, model.RouteModeAutoLab, preview.Mode)
	assert.Zero(t, preview.ProfileID)
	assert.Equal(t, []int{channel}, preview.CandidateChannelIDs)
	assert.NotEmpty(t, preview.PreviewID)
	assert.NotZero(t, preview.GeneratedAt)
	assert.Equal(t, before, ReadGatewayAdmissionRuntime(ChannelAdmissionKey(channel)))
	var profiles int64
	require.NoError(t, db.Model(&model.UserRouteProfile{}).Count(&profiles).Error)
	assert.Zero(t, profiles, "preview must not create an automatic profile")
	_, err = PreviewUserTokenRoute(context.Background(), user+1, token, input)
	assert.ErrorIs(t, err, ErrRouteProfileNotFound)
	input.Request = []byte(`{"model":"other-model"}`)
	_, err = PreviewUserTokenRoute(context.Background(), user, token, input)
	assert.ErrorIs(t, err, ErrRouteProfileValidation)
}

func TestManualRetryScopeStartsAfterFirstDispatch(t *testing.T) {
	for _, mode := range []string{model.RoutePolicyRetryNone, model.RoutePolicyRetrySameChannel} {
		selection := LiveRouteSelection{Retry: RouteLiveRetryPolicy{Mode: mode, MaxSameResourceAttempts: 1}}
		class := RouteErrorClassification{Retryable: true, Failoverable: true}
		assert.True(t, selection.AllowsCandidate(2, 0, class, RouteRetryCounters{}))
		assert.False(t, selection.AllowsCandidate(3, 2, class, RouteRetryCounters{TotalAttempts: 1}))
		assert.Equal(t, mode == model.RoutePolicyRetrySameChannel, selection.AllowsCandidate(2, 2, class, RouteRetryCounters{TotalAttempts: 1}))
		assert.False(t, selection.AllowsCandidate(2, 2, class, RouteRetryCounters{TotalAttempts: 2, SameChannelAttempts: 1}))
	}
}

func TestPreviewGeminiPathMustMatchRequestModel(t *testing.T) {
	_, err := AnalyzeRouteRequest("gemini-a", "/v1beta/models/gemini-b:generateContent", []byte(`{}`))
	require.ErrorContains(t, err, "path model")
	_, err = AnalyzeRouteRequest("gemini-a", "/v1beta/models/gemini-a:unsupported", []byte(`{}`))
	require.ErrorContains(t, err, "generation protocol")
	requirements, err := AnalyzeRouteRequest("gemini-a", "/v1beta/models/gemini-a:streamGenerateContent", []byte(`{}`))
	require.NoError(t, err)
	assert.True(t, requirements.Stream)
}
