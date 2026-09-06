package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaleInstanceCleanupUsesLatestHeartbeat(t *testing.T) {
	truncateTables(t)
	const now int64 = 1700000000
	require.NoError(t, UpsertSystemInstance("recovered", nil, now-3600, now-120))
	require.NoError(t, UpsertSystemInstance("stale", nil, now-3600, now-120))
	require.NoError(t, UpsertSystemInstance("boundary", nil, now-3600, now-90))

	listed, err := ListSystemInstances()
	require.NoError(t, err)
	require.Len(t, listed, 3)
	for _, instance := range listed {
		if instance.NodeName == "recovered" {
			assert.Equal(t, SystemInstanceStatusStale, instance.ToResponse(now).Status)
		}
	}

	// A stale row in the UI may recover before its confirmation is submitted.
	require.NoError(t, UpsertSystemInstance("recovered", nil, now-3600, now))
	deleted, err := DeleteStaleSystemInstance("recovered", now)
	require.NoError(t, err)
	assert.False(t, deleted)
	deleted, err = DeleteStaleSystemInstance("boundary", now)
	require.NoError(t, err)
	assert.False(t, deleted)
	count, err := DeleteStaleSystemInstances(now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
	remaining, err := ListSystemInstances()
	require.NoError(t, err)
	require.Len(t, remaining, 2)
	assert.ElementsMatch(t, []string{"recovered", "boundary"}, []string{remaining[0].NodeName, remaining[1].NodeName})
	deleted, err = DeleteStaleSystemInstance("recovered", now+91)
	require.NoError(t, err)
	assert.True(t, deleted)
}
