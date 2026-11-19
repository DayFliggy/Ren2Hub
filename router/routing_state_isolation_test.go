package router

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// releaseTestRoutingState treats the test database's channels and users as
// deleted when the test ends. The concurrency limits, route observations,
// breakers and cooldowns live in service for the whole process, while every
// test database numbers its rows from 1 again; without this, a later test's
// channel 1 or user 1 inherits an earlier test's open breaker or cached limit.
// It runs the cleanup production runs after deleting channels and users, so
// it must be registered after the cleanup that closes the database.
func releaseTestRoutingState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		var channelIDs, userIDs []int
		require.NoError(t, model.DB.Model(&model.Channel{}).Pluck("id", &channelIDs).Error)
		require.NoError(t, model.DB.Model(&model.User{}).Pluck("id", &userIDs).Error)
		service.ReleaseDeletedChannels(context.Background(), channelIDs)
		for _, id := range userIDs {
			service.ReleaseDeletedUser(id)
		}
	})
}
