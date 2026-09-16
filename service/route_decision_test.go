package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRouteSourceUsesAutomaticDefaultAndRejectsInvalidModes(t *testing.T) {
	assert.Equal(t, RouteSourceUnavailable, ResolveRouteSource(RouteSourceInput{}))
	assert.Equal(t, RouteSourceAutoLab, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true}))
	assert.Equal(t, RouteSourceUnavailable, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "legacy"}))
	assert.Equal(t, RouteSourceManual, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "manual"}))
	assert.Equal(t, RouteSourceAutoLab, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "auto_lab"}))
	assert.Equal(t, RouteSourceUnavailable, ResolveRouteSource(RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: "future"}))
}

func TestRouteDecisionKeepsExplainabilityFields(t *testing.T) {
	decision := NewRouteDecision("request-1", RouteSourceManual, "gpt-5", 3)
	decision.SelectedChannelID = 101
	decision.Candidates = append(decision.Candidates, RouteDecisionCandidate{ChannelID: 101, Priority: 20, Position: 0, AdmissionState: "acquired"})
	decision.SetFinalError(RouteErrorTransient)
	assert.Equal(t, "route_decision", decision.Event)
	assert.Equal(t, "acquired", decision.Candidates[0].AdmissionState)
	assert.Equal(t, string(RouteErrorTransient), decision.FinalError)
}

func TestRouteDecisionReadsHistoricalAdmissionField(t *testing.T) {
	for _, input := range []string{
		`{"lease_state":"released","candidates":[{"channel_id":1,"lease_state":"acquired"}]}`,
		`{"admission_state":"released","candidates":[{"channel_id":1,"admission_state":"acquired"}]}`,
	} {
		var decision RouteDecision
		require.NoError(t, common.Unmarshal([]byte(input), &decision))
		assert.Equal(t, "released", decision.AdmissionState)
		require.Len(t, decision.Candidates, 1)
		assert.Equal(t, "acquired", decision.Candidates[0].AdmissionState)
		encoded, err := common.Marshal(decision)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "lease_state")
		assert.Contains(t, string(encoded), "admission_state")
	}
}
