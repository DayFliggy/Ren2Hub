package service

import (
	"bytes"
	"cmp"
	"maps"
	"math/rand/v2"
	"slices"
	"time"
)

func compareCacheBands(a, b routeCacheBand) int {
	return cmp.Or(cmp.Compare(a.Model, b.Model), cmp.Compare(a.Operation, b.Operation), cmp.Compare(a.Mode, b.Mode), cmp.Compare(a.ChannelID, b.ChannelID), cmp.Compare(a.MappedModel, b.MappedModel), cmp.Compare(boolInt(a.Stream), boolInt(b.Stream)))
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

type cacheWindow struct {
	key        cacheRowKey
	totals     cacheCounts
	strata     map[cacheStratum]cacheCounts
	cohorts    map[cacheCohort]cacheCounts
	sessions   map[[16]byte]bool
	last       time.Time
	incomplete bool
}

func summarizeCache(key cacheRowKey, row *cacheRow, now, since time.Time) *cacheWindow {
	w := &cacheWindow{key: key, last: row.qualified, strata: map[cacheStratum]cacheCounts{}, cohorts: map[cacheCohort]cacheCounts{}, sessions: map[[16]byte]bool{}}
	for i := range row.buckets {
		b := &row.buckets[i]
		if b.tick <= cacheTick(now)-cachePeriods || b.tick > cacheTick(now) || (!since.IsZero() && b.tick < cacheTick(since)+1) {
			continue
		}
		w.totals.add(b.counts)
		w.incomplete = w.incomplete || b.incomplete
		for key, v := range b.cells {
			val := w.cohorts[key]
			val.add(v)
			w.cohorts[key] = val
			val = w.strata[key.stratum]
			val.add(v)
			w.strata[key.stratum] = val
			if v.valid > 0 {
				w.sessions[key.session] = true
			}
		}
	}
	return w
}
func (w *cacheWindow) eligible(now time.Time) bool {
	return !w.incomplete && now.Sub(w.last) <= 15*time.Minute && w.totals.valid >= cacheRequestMinimum && w.totals.maxInput <= 0.05*w.totals.input && w.totals.valid*5 >= w.totals.count*4 && len(w.sessions) >= cacheSessionMinimum
}
func (w *cacheWindow) diagnostic() routeCacheBand {
	k, t := w.key, w.totals
	band := routeCacheBand{ChannelID: k.channel, Model: k.group.Model, MappedModel: k.mapped, Mode: k.group.Mode, Operation: k.group.Operation, Stream: k.group.Stream, Samples: t.valid, Sessions: len(w.sessions), Missing: max(0, cacheRequestMinimum-t.valid), Reason: "insufficient_evidence"}
	if t.count > 0 {
		band.Coverage = float64(t.valid) / float64(t.count)
	}
	if t.valid > 0 {
		band.HitRate = float64(t.hits) / float64(t.valid)
		band.TokenRate = t.read / t.input
	}
	if w.incomplete {
		band.Reason = "capacity_exceeded"
	}
	return band
}

func (s *cacheStore) evaluate(now time.Time) {
	started := time.Now()
	s.mu.Lock()
	if !s.at.IsZero() && cacheTick(s.at) >= cacheTick(now) {
		s.mu.Unlock()
		return
	}
	revision := s.revision
	rows := make(map[cacheRowKey]*cacheRow, len(s.rows))
	for k, row := range s.rows {
		if now.Sub(row.last) > time.Hour {
			for i := range row.buckets {
				s.cells -= len(row.buckets[i].cells)
			}
			delete(s.rows, k)
			delete(s.state, k)
			continue
		}
		clone := *row
		for i := range clone.buckets {
			clone.buckets[i].cells = maps.Clone(clone.buckets[i].cells)
		}
		rows[k] = &clone
	}
	states := maps.Clone(s.state)
	s.mu.Unlock()
	groups := map[cacheGroup][]*cacheWindow{}
	for k, row := range rows {
		groups[k.group] = append(groups[k.group], summarizeCache(k, row, now, time.Time{}))
	}
	result := map[cacheGroup]map[int]routeCacheBand{}
	for g, group := range groups {
		result[g] = make(map[int]routeCacheBand, len(group))
		slices.SortFunc(group, func(a, b *cacheWindow) int { return cmp.Compare(a.key.channel, b.key.channel) })
		for _, row := range group {
			band := row.diagnostic()
			level := states[row.key]
			if row.eligible(now) {
				if peer, _, weights := bestCachePeer(row, group, now); peer != nil {
					low, _, ok := cacheInterval(row, peer, weights)
					if ok {
						band.Known = true
						band.Reference = peer.key.channel
						band.Reason = "comparable"
						next := 0
						if low >= 0.4-1e-12 {
							next = 2
						} else if low >= 0.2-1e-12 {
							next = 1
						}
						if next > level.band {
							witnesses := comparableSessions(row, weights)
							peerWitnesses := comparableSessions(peer, weights)
							if next == level.pending && cacheTick(now) == cacheTick(level.previous)+1 && hasFreshSession(witnesses, level.witnesses) && hasFreshSession(peerWitnesses, level.peerWitnesses) {
								level = cacheLevel{band: next, changed: now}
							} else if next != level.pending || cacheTick(now) != cacheTick(level.previous)+1 {
								level.pending = next
								level.previous = now
								level.witnesses = witnesses
								level.peerWitnesses = peerWitnesses
							} else {
								// Carry pending counts, but never confirm using the same sessions again.
								level.previous = now
							}
						} else {
							level.pending = 0
							level.witnesses = nil
							level.peerWitnesses = nil
						}
						if next < level.band && now.Sub(level.changed) >= 15*time.Minute {
							if recoveredCache(row, group, rows, level.changed, now) {
								level = cacheLevel{changed: now}
							}
						}
					} else {
						band.Reason = "concentrated_or_sparse"
					}
				} else {
					band.Reason = "no_comparable_peer"
				}
			}
			if !band.Known {
				level.pending = 0
				level.witnesses = nil
				level.peerWitnesses = nil
			}
			states[row.key] = level
			band.Band = level.band
			result[g][row.key.channel] = band
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision != revision {
		return
	}
	s.state = states
	s.result = result
	s.at = now
	s.duration = time.Since(started)
}

func hasFreshSession(current, previous map[[16]byte]bool) bool {
	for key := range current {
		if !previous[key] {
			return true
		}
	}
	return false
}
func comparableSessions(w *cacheWindow, weights map[cacheStratum]float64) map[[16]byte]bool {
	m := map[[16]byte]bool{}
	for k := range w.cohorts {
		if weights[k.stratum] > 0 && w.cohorts[k].valid > 0 {
			m[k.session] = true
		}
	}
	return m
}
func recoveredCache(row *cacheWindow, group []*cacheWindow, rows map[cacheRowKey]*cacheRow, since, now time.Time) bool {
	current := summarizeCache(row.key, rows[row.key], now, since)
	if !current.eligible(now) {
		return false
	}
	recent := make([]*cacheWindow, 0, len(group))
	for _, peer := range group {
		if peer.key != row.key {
			recent = append(recent, summarizeCache(peer.key, rows[peer.key], now, since))
		}
	}
	peer, _, weights := bestCachePeer(current, recent, now)
	if peer == nil {
		return false
	}
	_, hi, ok := cacheInterval(current, peer, weights)
	return ok && hi < 0.1
}

// Direct standardization uses the same request mix on both sides. A warm overlap
// must exist: a stream of exclusively first visits says nothing about cache reuse.
func bestCachePeer(row *cacheWindow, group []*cacheWindow, now time.Time) (*cacheWindow, float64, map[cacheStratum]float64) {
	var best *cacheWindow
	var bestDelta float64
	var bestWeights map[cacheStratum]float64
	for _, peer := range group {
		if peer.key.channel == row.key.channel || !peer.eligible(now) {
			continue
		}
		sum, delta := 0., 0.
		weights := map[cacheStratum]float64{}
		var aCount, bCount int
		warm := false
		for k, a := range row.strata {
			b := peer.strata[k]
			if min(a.input, b.input) == 0 || a.valid*5 < a.count*4 || b.valid*5 < b.count*4 {
				continue
			}
			weight := float64(a.valid + b.valid)
			sum += weight
			delta += weight * (b.read/b.input - a.read/a.input)
			weights[k] = weight
			aCount += a.valid
			bCount += b.valid
			warm = warm || k.warm
		}
		if !warm || min(aCount, bCount) < cacheRequestMinimum {
			continue
		}
		delta /= sum
		if best == nil || delta > bestDelta {
			for k, weight := range weights {
				weights[k] = weight / sum
			}
			best = peer
			bestDelta = delta
			bestWeights = weights
		}
	}
	return best, bestDelta, bestWeights
}

type weightedSessionCell struct {
	index       int
	read, input float64
}

// Resample whole sessions, preserving correlation between repeats and strata.
// Invalid bootstrap replicates (loss of a whole stratum) cannot establish a bound.
func cacheInterval(row, peer *cacheWindow, weights map[cacheStratum]float64) (float64, float64, bool) {
	strata := slices.Collect(maps.Keys(weights))
	slices.SortFunc(strata, func(a, b cacheStratum) int {
		return cmp.Or(cmp.Compare(a.length, b.length), cmp.Compare(a.source, b.source), cmp.Compare(boolInt(a.warm), boolInt(b.warm)))
	})
	sessions := func(w *cacheWindow) ([][]weightedSessionCell, bool) {
		cells := make(map[[16]byte][]weightedSessionCell)
		for i, k := range strata {
			st := w.strata[k]
			if (st.maxInput/st.input)*weights[k] > 0.05+1e-12 {
				return nil, false
			}
			for c, v := range w.cohorts {
				if c.stratum == k && v.valid > 0 {
					cells[c.session] = append(cells[c.session], weightedSessionCell{i, v.read, v.input})
				}
			}
		}
		if len(cells) < cacheSessionMinimum {
			return nil, false
		}
		ids := slices.Collect(maps.Keys(cells))
		slices.SortFunc(ids, func(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) })
		result := make([][]weightedSessionCell, 0, len(ids))
		for _, id := range ids {
			share := 0.
			for _, v := range cells[id] {
				k := strata[v.index]
				share += weights[k] * v.input / w.strata[k].input
			}
			if share > 0.20+1e-12 {
				return nil, false
			}
			result = append(result, cells[id])
		}
		return result, true
	}
	a, okA := sessions(row)
	b, okB := sessions(peer)
	if !okA || !okB {
		return 0, 0, false
	}
	rng := rand.New(rand.NewPCG(437, 1901))
	read := make([]float64, len(strata))
	input := make([]float64, len(strata))
	draw := func(clusters [][]weightedSessionCell) (float64, bool) {
		clear(read)
		clear(input)
		for range clusters {
			for _, v := range clusters[rng.IntN(len(clusters))] {
				read[v.index] += v.read
				input[v.index] += v.input
			}
		}
		rate := 0.
		for i, k := range strata {
			if input[i] == 0 {
				return 0, false
			}
			rate += weights[k] * read[i] / input[i]
		}
		return rate, true
	}
	const repetitions = 256
	deltas := make([]float64, 0, repetitions)
	for range repetitions {
		x, okX := draw(a)
		y, okY := draw(b)
		if okX && okY {
			deltas = append(deltas, y-x)
		}
	}
	if len(deltas) < repetitions*95/100 {
		return 0, 0, false
	}
	slices.Sort(deltas)
	return deltas[len(deltas)*25/1000], deltas[len(deltas)*975/1000], true
}
