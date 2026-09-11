package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimulateKeepsStickyInsidePriorityBoundary(t *testing.T) {
	output, err := simulate(simulationInput{
		Mode: "auto_lab", RequestID: "simulator", RequestModel: "gpt-5", DynamicScoringEnabled: true,
		Candidates: []simulationCandidate{
			{ChannelID: 1, Priority: 10, Weight: 1000, HealthUsable: true},
			{ChannelID: 2, Priority: 10, Weight: 1, Sticky: true, HealthUsable: true},
			{ChannelID: 3, Priority: 5, Weight: 1000, Sticky: true, HealthUsable: true},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []int{2, 1, 3}, output.CandidateChannelIDs)
}
