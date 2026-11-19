package service

import (
	"maps"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	qualityInterval    = 5 * time.Minute
	qualityFresh       = 15 * time.Minute
	qualityCapacity    = 4096
	qualityLatencyBins = 48
)

type routeQualityKey struct {
	requested string
	routeObservationKey
}
type qualityBucket struct {
	period                                     int64
	total, successes, latencyCount, speedCount uint32
	latency                                    [qualityLatencyBins]uint32
	outputTokens                               int64
	generation                                 time.Duration
}
type qualityHistory struct {
	buckets    [12]qualityBucket
	updated    time.Time
	degradedAt time.Time
	band       int
	reason     string
}
type RouteQualitySnapshot struct {
	ChannelID       int     `json:"channel_id"`
	Model           string  `json:"model"`
	ActualModel     string  `json:"actual_model"`
	Operation       int     `json:"operation"`
	Stream          bool    `json:"stream"`
	Band            int     `json:"band"`
	Reason          string  `json:"reason"`
	Samples         int     `json:"samples"`
	SuccessRate     float64 `json:"success_rate"`
	LatencySamples  int     `json:"latency_samples"`
	LatencyMS       float64 `json:"latency_ms"`
	SpeedSamples    int     `json:"speed_samples"`
	TokensPerSecond float64 `json:"tokens_per_second"`
	UpdatedAt       int64   `json:"updated_at"`
	EvaluatedAt     int64   `json:"evaluated_at"`
}
type qualitySnapshot map[routeQualityKey]RouteQualitySnapshot
type routeQualityStore struct {
	mu        sync.Mutex
	history   map[routeQualityKey]*qualityHistory
	published atomic.Pointer[qualitySnapshot]
	evictions uint64
}

func newRouteQualityStore() *routeQualityStore {
	store := &routeQualityStore{history: make(map[routeQualityKey]*qualityHistory)}
	rows := qualitySnapshot{}
	store.published.Store(&rows)
	return store
}

var routeQualities = newRouteQualityStore()

// StartRoutingEvaluation publishes immutable snapshots for selectors and diagnostics.
func StartRoutingEvaluation() func() {
	done := make(chan struct{})
	var stop sync.Once
	go func() {
		ticker := time.NewTicker(qualityInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				routeQualities.evaluate(now)
				routeCaches.evaluate(now)
			case <-done:
				return
			}
		}
	}()
	return func() { stop.Do(func() { close(done) }) }
}

func recordRouteQuality(epoch uint64, requested string, key routeObservationKey, success bool, latencyMS, outputTokens int64, generation time.Duration) {
	if key.id <= 0 || key.model == "" || requested == "" {
		return
	}
	routeObservations.mu.Lock()
	defer routeObservations.mu.Unlock()
	if routeObservations.staleLocked(epoch, key.id) {
		return
	}
	routeQualities.add(routeQualityKey{requested, key}, success, latencyMS, outputTokens, generation, time.Now())
}
func (s *routeQualityStore) snapshot() qualitySnapshot { return *s.published.Load() }
func (s *routeQualityStore) invalidate(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.history, func(k routeQualityKey, _ *qualityHistory) bool { return k.id == id })
	rows := maps.Clone(s.snapshot())
	maps.DeleteFunc(rows, func(k routeQualityKey, _ RouteQualitySnapshot) bool { return k.id == id })
	s.published.Store(&rows)
}
func (s *routeQualityStore) add(key routeQualityKey, success bool, latencyMS, outputTokens int64, generation time.Duration, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.history[key]
	if h == nil {
		if len(s.history) >= qualityCapacity {
			var oldest routeQualityKey
			var at time.Time
			for k, entry := range s.history {
				if at.IsZero() || entry.updated.Before(at) {
					oldest, at = k, entry.updated
				}
			}
			delete(s.history, oldest)
			s.evictions++
		}
		h = &qualityHistory{}
		s.history[key] = h
	}
	h.updated = now
	period := now.Unix() / int64(qualityInterval/time.Second)
	b := &h.buckets[period%int64(len(h.buckets))]
	if b.period != period {
		*b = qualityBucket{period: period}
	}
	b.total++
	if !success {
		return
	}
	b.successes++
	if latencyMS > 0 {
		index := int(math.Round(math.Log(float64(latencyMS)/25) / math.Log(1.25)))
		b.latency[min(qualityLatencyBins-1, max(index, 0))]++
		b.latencyCount++
	}
	if outputTokens >= 50 && generation >= 500*time.Millisecond {
		b.outputTokens += outputTokens
		b.generation += generation
		b.speedCount++
	}
}
func (h *qualityHistory) summary(key routeQualityKey, now, since time.Time) RouteQualitySnapshot {
	row := RouteQualitySnapshot{ChannelID: key.id, Model: key.requested, ActualModel: key.model, Operation: key.operation, Stream: key.stream, UpdatedAt: h.updated.UnixMilli(), EvaluatedAt: now.UnixMilli()}
	period := now.Unix() / int64(qualityInterval/time.Second)
	var bins [qualityLatencyBins]uint64
	var successes, output int64
	var duration time.Duration
	for _, b := range h.buckets {
		if b.total == 0 || b.period > period || period-b.period >= 12 {
			continue
		}
		// A bucket overlapping the downgrade predates the recovery round.
		if !since.IsZero() && !time.Unix(b.period*300, 0).After(since) {
			continue
		}
		row.Samples += int(b.total)
		successes += int64(b.successes)
		row.LatencySamples += int(b.latencyCount)
		row.SpeedSamples += int(b.speedCount)
		for i, n := range b.latency {
			bins[i] += uint64(n)
		}
		output += b.outputTokens
		duration += b.generation
	}
	if row.Samples > 0 {
		row.SuccessRate = float64(successes) / float64(row.Samples)
	}
	if row.LatencySamples > 0 {
		var cumulative uint64
		for i, count := range bins {
			cumulative += count
			if cumulative*2 >= uint64(row.LatencySamples) {
				row.LatencyMS = 25 * math.Pow(1.25, float64(i))
				break
			}
		}
	}
	if duration > 0 {
		row.TokensPerSecond = float64(output) / duration.Seconds()
	}
	return row
}

type qualityGroup struct {
	requested string
	operation int
	stream    bool
}
type qualityReference struct {
	success               RouteQualitySnapshot
	fastestMS, fastestTPS float64
}

func (r *qualityReference) include(row RouteQualitySnapshot) {
	if row.Samples >= 50 && (r.success.Samples == 0 || row.SuccessRate > r.success.SuccessRate || row.SuccessRate == r.success.SuccessRate && row.Samples > r.success.Samples) {
		r.success = row
	}
	if row.LatencySamples >= 20 && (r.fastestMS == 0 || row.LatencyMS < r.fastestMS) {
		r.fastestMS = row.LatencyMS
	}
	if row.SpeedSamples >= 20 {
		r.fastestTPS = max(r.fastestTPS, min(100, row.TokensPerSecond))
	}
}
func qualityInterval95(rate float64, n int) (float64, float64) {
	const z = 1.96
	count := float64(n)
	center := rate + z*z/(2*count)
	margin := z * math.Sqrt((rate*(1-rate)+z*z/(4*count))/count)
	return (center - margin) / (1 + z*z/count), (center + margin) / (1 + z*z/count)
}
func classifyRouteQuality(row RouteQualitySnapshot, r qualityReference) (int, string) {
	band, reason := 0, "steady"
	if row.Samples < 50 && row.LatencySamples < 20 && row.SpeedSamples < 20 {
		reason = "sampling"
	}
	if row.Samples >= 50 && r.success.Samples >= 50 {
		_, hi := qualityInterval95(row.SuccessRate, row.Samples)
		lo, _ := qualityInterval95(r.success.SuccessRate, r.success.Samples)
		gap := r.success.SuccessRate - row.SuccessRate
		if hi < lo && gap >= .05 {
			band, reason = 1, "success_rate"
			if gap >= .2 {
				band = 2
			}
		}
	}
	if row.LatencySamples >= 20 && r.fastestMS > 0 {
		degree := 0
		if row.LatencyMS >= 2*r.fastestMS && row.LatencyMS-r.fastestMS >= 1000 {
			degree = 1
		}
		if row.LatencyMS >= 4*r.fastestMS && row.LatencyMS-r.fastestMS >= 3000 {
			degree = 2
		}
		if degree > band {
			band, reason = degree, "latency"
		}
	}
	if row.SpeedSamples >= 20 && r.fastestTPS > 0 {
		speed := min(100, row.TokensPerSecond)
		degree := 0
		if speed <= .5*r.fastestTPS && r.fastestTPS-speed >= 10 {
			degree = 1
		}
		if degree > 0 && speed <= .25*r.fastestTPS {
			degree = 2
		}
		if degree > band {
			band, reason = degree, "speed"
		}
	}
	return band, reason
}

// Recovery requires fresh measurements and a margin on the metric that caused the downgrade.
func routeQualityRecovered(fresh RouteQualitySnapshot, ref qualityReference, reason string) bool {
	if fresh.Samples < 50 || ref.success.Samples < 50 || ref.success.SuccessRate-fresh.SuccessRate >= .02 {
		return false
	}
	if band, _ := classifyRouteQuality(fresh, ref); band != 0 {
		return false
	}
	switch reason {
	case "latency":
		return fresh.LatencySamples >= 20 && ref.fastestMS > 0 && fresh.LatencyMS < 1.6*ref.fastestMS
	case "speed":
		return fresh.SpeedSamples >= 20 && ref.fastestTPS > 0 && fresh.TokensPerSecond > .625*ref.fastestTPS
	default:
		return true
	}
}

func (s *routeQualityStore) evaluate(now time.Time) {
	// Read health before taking the quality lock: configuration publication locks in that order.
	breakers, cooldowns := routeObservations.healthSnapshot()
	paused := make(map[routeBreakerKey]bool, len(breakers))
	for _, b := range breakers {
		paused[routeBreakerKey{b.ChannelID, b.Model}] = true
	}
	cooling := make(map[int]bool, len(cooldowns))
	for _, c := range cooldowns {
		cooling[c.ChannelID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := qualitySnapshot{}
	refs := map[qualityGroup]qualityReference{}
	for k, h := range s.history {
		if now.Sub(h.updated) > 2*time.Hour {
			delete(s.history, k)
			continue
		}
		row := h.summary(k, now, time.Time{})
		rows[k] = row
		if now.Sub(h.updated) > qualityFresh || cooling[k.id] || paused[routeBreakerKey{k.id, k.model}] {
			continue
		}
		group := qualityGroup{k.requested, k.operation, k.stream}
		ref := refs[group]
		ref.include(row)
		refs[group] = ref
	}
	for k, row := range rows {
		h := s.history[k]
		ref := refs[qualityGroup{k.requested, k.operation, k.stream}]
		band, reason := classifyRouteQuality(row, ref)
		if h.band > 0 && band <= h.band {
			fresh := h.summary(k, now, h.degradedAt)
			if now.Sub(h.degradedAt) >= qualityFresh && routeQualityRecovered(fresh, ref, h.reason) {
				band, reason = 0, "steady"
				for i, bucket := range h.buckets {
					if !time.Unix(bucket.period*300, 0).After(h.degradedAt) {
						h.buckets[i] = qualityBucket{}
					}
				}
				row = h.summary(k, now, time.Time{})
			} else if band < h.band {
				band, reason = h.band, h.reason
			}
		}
		if band > h.band {
			h.degradedAt = now
		}
		h.band, h.reason = band, reason
		row.Band, row.Reason = band, reason
		rows[k] = row
	}
	s.published.Store(&rows)
}
func qualityDiagnostics() []RouteQualitySnapshot {
	rows := routeQualities.snapshot()
	out := make([]RouteQualitySnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ChannelID != b.ChannelID {
			return a.ChannelID < b.ChannelID
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Operation != b.Operation {
			return a.Operation < b.Operation
		}
		return !a.Stream && b.Stream
	})
	return out
}

func RouteChannelQualitySummaries() map[int]*model.ChannelRoutingQuality {
	summaries := make(map[int]*model.ChannelRoutingQuality)
	update := func(id int, modelName string, band int, reason string) {
		previous := summaries[id]
		if previous == nil || band > previous.Band || band == previous.Band && (modelName < previous.Model || modelName == previous.Model && reason < previous.Reason) {
			summaries[id] = &model.ChannelRoutingQuality{Model: modelName, Band: band, Reason: reason}
		}
	}
	for _, row := range routeQualities.snapshot() {
		if row.Samples > 0 {
			update(row.ChannelID, row.Model, row.Band, row.Reason)
		}
	}
	for _, row := range routeCaches.diagnostics().Bands {
		if row.Known && row.Band > 0 {
			update(row.ChannelID, row.Model, row.Band, "cache")
		}
	}
	return summaries
}
