package service

import (
	"encoding/binary"
	"slices"
	"time"
)

type cacheBudget struct {
	tick           int64
	requests, used int
}

func (s *cacheStore) budgetTotals(tick int64) (int, int) {
	var requests, used int
	for _, bucket := range s.budget {
		if bucket.tick > tick-cachePeriods && bucket.tick <= tick {
			requests += bucket.requests
			used += bucket.used
		}
	}
	return requests, used
}

// Call once per free initial selection, never from previews or a retry. Existing
// assignments may consume the remaining hourly budget; new assignments then wait.
func (s *cacheStore) explore(ctx routeCacheRequest, model string, op int, stream bool, eligible, deficient []int, now time.Time) int {
	if ctx.resource || len(eligible) == 0 {
		return 0
	}
	group := cacheGroup{model, op, stream, ctx.mode}
	s.mu.Lock()
	defer s.mu.Unlock()
	tick := cacheTick(now)
	period := &s.budget[tick%cachePeriods]
	if period.tick != tick {
		*period = cacheBudget{tick: tick}
	}
	period.requests++
	identified := ctx.identifiable()
	key := cacheVisitKey{group, ctx.session, 0}
	visit := s.lookup(key, now)
	if identified && visit != nil {
		if visit.exploring != 0 && slices.Contains(eligible, visit.exploring) {
			period.used++
			return visit.exploring
		}
		return 0
	}
	requests, used := s.budgetTotals(tick)
	if len(eligible) < 2 || len(deficient) == 0 || (used+1)*20 > requests {
		return 0
	}
	// Rotate anonymous probes for success and latency. Identified new sessions
	// get a stable channel to preserve the opportunity for a warm follow-up.
	start := used % len(deficient)
	if identified {
		start = int(binary.BigEndian.Uint16(ctx.session[:2])) % len(deficient)
	}
	for i := range deficient {
		id := deficient[(start+i)%len(deficient)]
		if slices.Contains(eligible, id) {
			period.used++
			if identified {
				s.remember(key, now, ctx.ttl, id)
			}
			return id
		}
	}
	return 0
}
