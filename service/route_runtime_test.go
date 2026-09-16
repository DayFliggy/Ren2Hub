package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouteRuntimeRecheckFencesChannelSettings(t *testing.T) {
	expected := RouteRuntimeState{
		ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3,
		Capacity: 4, ChannelRatio: 1,
	}
	tests := []struct {
		name    string
		current RouteRuntimeState
	}{
		{name: "disabled", current: RouteRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, ChannelRatio: 1}},
		{name: "ratio missing", current: RouteRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, Capacity: 4}},
		{name: "health epoch changed", current: RouteRuntimeState{ChannelEnabled: true, HealthEpoch: 3, CapabilityVersion: 3, Capacity: 4, ChannelRatio: 1}},
		{name: "capability changed", current: RouteRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 4, Capacity: 4, ChannelRatio: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.ErrorIs(t, RecheckRouteRuntime(expected, test.current), ErrAdmissionRuntime)
		})
	}
	current := expected
	current.Capacity++
	assert.NoError(t, RecheckRouteRuntime(expected, current), "a granted permit survives a capacity-only update")
}
