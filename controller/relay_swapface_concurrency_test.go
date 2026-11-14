package controller_test

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
)

// Native NewAPI has no local admission counters. Keep this local contract
// separate from the byte-identical native/current request-plugin comparison.
func TestSwapFaceRequestPluginRejectionReleasesConcurrency(t *testing.T) {
	for _, global := range []bool{true, false} {
		name := "channel_ingress"
		if global {
			name = "global_ingress"
		}
		t.Run(name, func(t *testing.T) {
			exerciseSwapFaceRequestPluginRejection(t, global, func(userID, channelID int) {
				snapshot := service.RoutingSnapshot()
				check := func(rows []service.ConcurrencySnapshot, id int, kind string) {
					found := false
					for _, row := range rows {
						if row.ID != id {
							continue
						}
						found = true
						assert.Equal(t, 1, row.Limit, kind+" admission must use the persisted limit")
						assert.Zero(t, row.InFlight, kind+" admission must release after a plugin rejection")
					}
					assert.True(t, found, kind+" must have traversed real admission")
				}
				check(snapshot.Users, userID, "user")
				check(snapshot.Channels, channelID, "channel")
				assert.Zero(t, service.ChannelInFlight(channelID))
				t.Cleanup(func() {
					service.ReleaseDeletedUser(userID)
					service.ReleaseDeletedChannels(context.Background(), []int{channelID})
				})
			})
		})
	}
}
