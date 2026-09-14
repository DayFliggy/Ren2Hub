package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGatewayConfigEnforcesWaitAndUserQueueBoundsAtEverySource(t *testing.T) {
	config := DefaultGatewayConfig()
	require.NoError(t, config.Validate())
	config.WaitMS = 5001
	require.Error(t, config.Validate())
	config.WaitMS, config.UserQueueSize = 5000, 21
	require.Error(t, config.Validate())
	for _, override := range []GatewayAdmissionOverride{{WaitMS: common.GetPointer(5001)}, {QueueSize: common.GetPointer(21)}} {
		require.Error(t, override.Validate())
		config = DefaultGatewayConfig()
		config.Groups["test"] = override
		require.Error(t, config.Validate())
	}
	require.NoError(t, (GatewayAdmissionOverride{}).Validate())
	require.NoError(t, (GatewayAdmissionOverride{Concurrency: common.GetPointer(0), WaitMS: common.GetPointer(0), QueueSize: common.GetPointer(0)}).Validate())
}
