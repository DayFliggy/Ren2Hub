package service

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
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
	breakers  map[routeBreakerKey]routeBreakerState
	cooldowns map[int]routeCooldown
	lastPrune int64
	epoch     uint64
	changed   map[int]uint64
	// staleFloor 是 changed 满容量清空时的 epoch：更早开始的尝试对所有渠道都按过期处理，
	// changed 因此有上限，不随删除或发布的渠道 ID 无限增长。
	staleFloor uint64
	// evicted 统计上次日志之后因容量淘汰的键数。
	evicted         int
	lastEvictionLog int64
	// Lock-free scalar read indexes updated on breaker/cooldown transitions and
	// channel invalidation so user catalog projection never acquires s.mu.
	breakerOpenUntil sync.Map      // routeBreakerKey -> int64 (non-zero openUntil)
	cooldownUntil    sync.Map      // int (channelID) -> int64 (until > 0)
	pendingMutations sync.Map      // int (channelID) -> *atomic.Int64 (active DB mutation refcount)
	pendingPublish   sync.Map      // int (channelID) -> uint64 (pending concurrency publication gen)
	configVersion    atomic.Uint64 // even when quiescent, odd during invalidation
}

var routeObservations = routeObservationStore{
	rows:      make(map[routeObservationKey]RouteObservationSnapshot),
	breakers:  make(map[routeBreakerKey]routeBreakerState),
	cooldowns: make(map[int]routeCooldown),
	changed:   make(map[int]uint64),
}

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
	s.configVersion.Add(1)
	defer s.configVersion.Add(1)
	if _, present := s.changed[channelID]; !present && len(s.changed) >= routeObservationCapacity {
		s.staleFloor = s.epoch
		clear(s.changed)
		common.SysLog(common.LogText("route observation epoch table reached %d channels: attempts started before epoch %d are treated as stale", routeObservationCapacity, s.staleFloor))
	}
	s.epoch++
	s.changed[channelID] = s.epoch
	for key := range s.rows {
		if key.id == channelID {
			delete(s.rows, key)
		}
	}
	for key := range s.breakers {
		if key.id == channelID {
			delete(s.breakers, key)
			s.breakerOpenUntil.Delete(key)
		}
	}
	s.breakerOpenUntil.Range(func(k, _ any) bool {
		if bk, ok := k.(routeBreakerKey); ok && bk.id == channelID {
			s.breakerOpenUntil.Delete(k)
		}
		return true
	})
	delete(s.cooldowns, channelID)
	s.cooldownUntil.Delete(channelID)
	routeQualities.invalidate(channelID)
	routeCaches.invalidate(channelID)
}

func (s *routeObservationStore) catalogBreakerOpenUntil(key routeBreakerKey) int64 {
	if value, ok := s.breakerOpenUntil.Load(key); ok {
		return value.(int64)
	}
	return 0
}

func (s *routeObservationStore) catalogCooldownUntil(channelID int) int64 {
	if value, ok := s.cooldownUntil.Load(channelID); ok {
		return value.(int64)
	}
	return 0
}

func (s *routeObservationStore) catalogConfigVersion() uint64 {
	return s.configVersion.Load()
}

// beginChannelMutation marks the channels as having an uncommitted or
// not-yet-published configuration mutation so catalog readers never attach
// pre-mutation health observations to newly committed channel rows.
func (s *routeObservationStore) beginChannelMutation(ids []int) func() {
	tracked := make([]*atomic.Int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		raw, _ := s.pendingMutations.LoadOrStore(id, &atomic.Int64{})
		counter := raw.(*atomic.Int64)
		counter.Add(1)
		tracked = append(tracked, counter)
	}
	if len(tracked) > 0 {
		s.configVersion.Add(2)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, counter := range tracked {
				counter.Add(-1)
			}
			if len(tracked) > 0 {
				s.configVersion.Add(2)
			}
		})
	}
}

func (s *routeObservationStore) markChannelPublishPending(channelID int, publication uint64) {
	if channelID <= 0 {
		return
	}
	s.pendingPublish.Store(channelID, publication)
	s.configVersion.Add(2)
}

func (s *routeObservationStore) clearChannelPublishPending(channelID int, publication uint64) {
	if channelID <= 0 {
		return
	}
	if current, ok := s.pendingPublish.Load(channelID); ok && current.(uint64) == publication {
		s.pendingPublish.Delete(channelID)
		s.configVersion.Add(2)
	}
}

func (s *routeObservationStore) clearDeletedChannelPending(channelID int) {
	if channelID <= 0 {
		return
	}
	s.pendingPublish.Delete(channelID)
	s.pendingMutations.Delete(channelID)
}

func (s *routeObservationStore) catalogChannelPending(channelID int) bool {
	if channelID <= 0 {
		return false
	}
	if _, pending := s.pendingPublish.Load(channelID); pending {
		return true
	}
	if raw, ok := s.pendingMutations.Load(channelID); ok && raw.(*atomic.Int64).Load() > 0 {
		return true
	}
	return false
}

// staleLocked 判断从 epoch 开始的尝试对该渠道是否已过期。调用方持有 s.mu。
func (s *routeObservationStore) staleLocked(epoch uint64, channelID int) bool {
	return epoch < s.staleFloor || s.changed[channelID] > epoch
}

// pruneLocked 每个过期周期最多清理一次过期的样本、熔断状态和 429 冷却。调用方持有 s.mu。
func (s *routeObservationStore) pruneLocked(now int64) {
	ttl := routeObservationTTL.Milliseconds()
	if now-s.lastPrune <= ttl {
		return
	}
	for key, row := range s.rows {
		if now-row.UpdatedAt > ttl {
			delete(s.rows, key)
		}
	}
	for key, state := range s.breakers {
		if state.openUntil == 0 && now-state.updatedAt > ttl {
			delete(s.breakers, key)
			s.breakerOpenUntil.Delete(key)
		}
	}
	for id, cooldown := range s.cooldowns {
		if now >= cooldown.until {
			delete(s.cooldowns, id)
			s.cooldownUntil.Delete(id)
		}
	}
	s.lastPrune = now
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
	if routeObservations.staleLocked(epoch, channelID) {
		return
	}
	routeObservations.pruneLocked(now)
	r, exists := routeObservations.rows[key]
	if !exists {
		evictForInsertLocked(&routeObservations, routeObservations.rows, func(v RouteObservationSnapshot) int64 { return v.UpdatedAt }, now)
	}
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
