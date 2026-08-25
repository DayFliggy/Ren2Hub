package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveRouteSourceFailsClosedToLegacy(t *testing.T) {
	assert.Equal(t, RouteSourceLegacy, ResolveRouteSource(RouteSourceInput{}))
	assert.Equal(t, RouteSourceLegacy, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "legacy"}))
	assert.Equal(t, RouteSourceManual, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"}))
	assert.Equal(t, RouteSourceAutoLab, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"}))
	assert.Equal(t, RouteSourceLegacy, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "future"}))
}

func TestRouteDecisionKeepsExplainabilityFields(t *testing.T) {
	decision := NewRouteDecision("request-1", RouteSourceManual, "gpt-5", 3)
	decision.SelectedChannelID = 101
	decision.Candidates = append(decision.Candidates, RouteDecisionCandidate{ChannelID: 101, Priority: 20, Position: 0, LeaseState: "acquired"})
	decision.SetFinalError(RouteErrorTransient)
	assert.Equal(t, "route_decision", decision.Event)
	assert.Equal(t, "acquired", decision.Candidates[0].LeaseState)
	assert.Equal(t, string(RouteErrorTransient), decision.FinalError)
}
