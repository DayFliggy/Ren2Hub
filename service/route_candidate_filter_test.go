package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestFilterRouteCapabilityTreatsLowConfidenceResolutionAsUnknown(t *testing.T) {
	result := filterRouteCapability(routeCapabilityFilterInput{
		Capability: model.ChannelModelCapability{
			ChannelID:  1,
			LabSlug:    "",
			Confidence: 0.89,
			Source:     "unknown",
			State:      model.RouteCapabilityStateUnresolved,
		},
		ChannelStatus:  common.ChannelStatusEnabled,
		AbilityEnabled: true,
		AbilityAllowed: true,
		Entitled:       true,
	})

	assert.Equal(t, ShadowFilterUnknownCapability, result.Reason)
}
