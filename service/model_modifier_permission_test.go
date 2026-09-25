package service

import (
	"context"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelModifierPermissionIsDirectional(t *testing.T) {
	assert.True(t, tokenAllowsRouteModel(map[string]bool{"gpt-5": true}, "gpt-5@effort:high"))
	assert.True(t, tokenAllowsRouteModel(map[string]bool{"gpt-5@effort:high": true}, "gpt-5@effort:high"))
	assert.False(t, tokenAllowsRouteModel(map[string]bool{"gpt-5@effort:high": true}, "gpt-5"))
	assert.False(t, tokenAllowsRouteModel(map[string]bool{"gpt-5@effort:high": true}, "gpt-5@effort:low"))
}

func TestLiveModifierRouteUsesBaseCapability(t *testing.T) {
	db := setupRouteProfileTest(t)
	useRoutePlanningRedis(t)
	user, token, channel := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channel, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	selection, err := SelectLiveTokenRoute(LiveRouteRequest{Context: context.Background(), CapabilityEnabled: true, UserID: user, TokenID: token, RequestModel: "gpt-test@effort:high", RequestPath: "/v1/chat/completions", TokenModelLimitEnabled: true, TokenModelLimit: map[string]bool{"gpt-test": true}})
	require.NoError(t, err)
	assert.Equal(t, channel, selection.Decision.SelectedChannelID)
	assert.Equal(t, "gpt-test", selection.Attempts[0].RequestModel)
}
