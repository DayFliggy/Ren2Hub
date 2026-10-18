package service

import (
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const routeObservationTTL = 5 * time.Minute
const routeMinimumSamples = 50

type RouteObservationSnapshot struct {
	ChannelID      int     `json:"channel_id"`
	Model          string  `json:"model"`
	Operation      int     `json:"operation"`
	Stream         bool    `json:"stream"`
	Samples        int     `json:"samples"`
	ErrorRate      float64 `json:"error_rate"`
	LatencyMS      float64 `json:"latency_ms"`
	LatencySamples int     `json:"latency_samples"`
	LatencyKnown   bool    `json:"latency_known"`
	Known          bool    `json:"known"`
	UpdatedAt      int64   `json:"updated_at"`
}
type routeObservationKey struct {
	id        int
	model     string
	operation int
	stream    bool
}
type routeObservationStore struct {
	mu        sync.Mutex
	rows      map[routeObservationKey]RouteObservationSnapshot
	lastPrune int64
	epoch     uint64
	changed   map[int]uint64
}

var routeObservations = routeObservationStore{rows: make(map[routeObservationKey]RouteObservationSnapshot), changed: make(map[int]uint64)}

// BeginRouteObservation must run before reading the selected channel configuration.
// A later publication invalidates even a request that has not reached transport yet.
func BeginRouteObservation(c *gin.Context) {
	routeObservations.mu.Lock()
	epoch := routeObservations.epoch
	routeObservations.mu.Unlock()
	c.Set("route_observation_epoch", epoch)
}

func (s *routeObservationStore) invalidate(channelID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	s.changed[channelID] = s.epoch
	for key := range s.rows {
		if key.id == channelID {
			delete(s.rows, key)
		}
	}
}

// Callers classify an actually sent attempt once. Local rejection and cancellation
// never reach this recorder. Empty samples remain distinguishable from healthy ones.
func RecordRouteObservation(channelID int, model string, operation int, stream, success bool, latencyMS int64) {
	routeObservations.mu.Lock()
	epoch := routeObservations.epoch
	routeObservations.mu.Unlock()
	recordRouteObservation(epoch, channelID, model, operation, stream, success, latencyMS)
}

func recordRouteObservation(epoch uint64, channelID int, model string, operation int, stream, success bool, latencyMS int64) {
	if channelID <= 0 || model == "" {
		return
	}
	now := time.Now().UnixMilli()
	key := routeObservationKey{channelID, model, operation, stream}
	routeObservations.mu.Lock()
	defer routeObservations.mu.Unlock()
	if routeObservations.changed[channelID] > epoch {
		return
	}
	if now-routeObservations.lastPrune > routeObservationTTL.Milliseconds() {
		for k, row := range routeObservations.rows {
			if now-row.UpdatedAt > routeObservationTTL.Milliseconds() {
				delete(routeObservations.rows, k)
			}
		}
		routeObservations.lastPrune = now
	}
	if _, exists := routeObservations.rows[key]; !exists && len(routeObservations.rows) >= 10000 {
		return
	}
	r := routeObservations.rows[key]
	if now-r.UpdatedAt > routeObservationTTL.Milliseconds() {
		r = RouteObservationSnapshot{ChannelID: channelID, Model: model, Operation: operation, Stream: stream}
	}
	errorSample := 0.0
	if !success {
		errorSample = 1
	}
	if r.Samples == 0 {
		r.ErrorRate = errorSample
	} else {
		r.ErrorRate = .2*errorSample + .8*r.ErrorRate
	}
	if success && latencyMS > 0 {
		r.LatencySamples++
		r.LatencyKnown = r.LatencySamples >= routeMinimumSamples
		if r.LatencyMS == 0 {
			r.LatencyMS = float64(latencyMS)
		} else {
			r.LatencyMS = .2*float64(latencyMS) + .8*r.LatencyMS
		}
	}
	r.Samples++
	r.Known = r.Samples >= routeMinimumSamples
	r.UpdatedAt = now
	routeObservations.rows[key] = r
}

func (s *routeObservationStore) get(channelID int, model string, operation int, stream bool) RouteObservationSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := routeObservationKey{channelID, model, operation, stream}
	r := s.rows[key]
	if time.Now().UnixMilli()-r.UpdatedAt > routeObservationTTL.Milliseconds() {
		delete(s.rows, key)
		return RouteObservationSnapshot{}
	}
	return r
}

func (s *routeObservationStore) snapshot() []RouteObservationSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	rows := make([]RouteObservationSnapshot, 0, len(s.rows))
	for key, r := range s.rows {
		if now-r.UpdatedAt > routeObservationTTL.Milliseconds() {
			delete(s.rows, key)
			continue
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		if rows[i].Model != rows[j].Model {
			return rows[i].Model < rows[j].Model
		}
		if rows[i].Operation != rows[j].Operation {
			return rows[i].Operation < rows[j].Operation
		}
		return !rows[i].Stream && rows[j].Stream
	})
	return rows
}
