package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestRouteQualityWindowAndRecovery(t *testing.T) {
	now := time.Unix(1800000000, 0)
	store := newRouteQualityStore()
	a := routeQualityKey{"requested", routeObservationKey{1, "alias", 1, false}}
	b := routeQualityKey{"requested", routeObservationKey{2, "other", 1, false}}
	for i := range 100 {
		store.add(a, i%20 >= 3, 500, 0, 0, now)
		store.add(b, true, 500, 0, 0, now)
	}
	store.evaluate(now)
	require.Equal(t, 1, store.snapshot()[a].Band)
	now = now.Add(10 * time.Minute)
	for range 100 {
		store.add(a, true, 500, 0, 0, now)
		store.add(b, true, 500, 0, 0, now)
	}
	store.evaluate(now)
	require.Equal(t, 1, store.snapshot()[a].Band)
	store.evaluate(now.Add(5 * time.Minute))
	require.Zero(t, store.snapshot()[a].Band)
}
func TestRouteRankCapacityPriceAndUnknownSeats(t *testing.T) {
	mk := func(id int, ratio string) *model.Channel {
		return &model.Channel{Id: id, ChannelRatio: ratio, Weight: common.GetPointer(uint(10))}
	}
	a, b := mk(101, "1"), mk(102, "2")
	cheap := 0
	for i := range 10000 {
		rows := []*model.Channel{a, b}
		orderAutomaticRoutes(rows, fmt.Sprint(i), "price-fixture", 1, false)
		if rows[0] == a {
			cheap++
		}
	}
	require.InDelta(t, 5405, cheap, 150)
	// Active requests are comparable across finite limits only as a fraction of capacity.
	a.MaxConcurrency, b.MaxConcurrency = 2, 100
	loads := []routeRank{
		{channel: a, active: 1, healthFactor: 1, ratio: 1, draw: .1},
		{channel: b, active: 8, healthFactor: 1, ratio: 1, draw: .9},
	}
	rankAutomatic(loads)
	require.Equal(t, b.Id, loads[0].channel.Id)
	b.MaxConcurrency = 0
	rankAutomatic(loads)
	require.Equal(t, a.Id, loads[0].channel.Id)
	require.Zero(t, loads[0].load)
	require.Zero(t, loads[1].load)
	a.MaxConcurrency = 0

	unknown := mk(103, "1")
	a.Priority = common.GetPointer(int64(10))
	unknown.Priority = common.GetPointer(int64(5))
	entries := []routeRank{{channel: a, cacheKnown: true, cacheBand: 2, healthFactor: 1, ratio: 1, draw: .4}, {channel: unknown, healthFactor: 1, ratio: 1, draw: .5}, {channel: b, cacheKnown: true, healthFactor: 1, ratio: 2, draw: .6}}
	rankAutomatic(entries)
	require.Equal(t, []int{b.Id, unknown.Id, a.Id}, []int{entries[0].channel.Id, entries[1].channel.Id, entries[2].channel.Id})
}
