package service

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 渠道级限流冷却和按实际模型的熔断分别计时。
const (
	routeBreakerOpenFailures   = 5
	routeBreakerBucketDuration = 10 * time.Second
	routeBreakerWindowSamples  = 20
	routeBreakerCooldown       = time.Minute
	routeHeaderless429Cooldown = 5 * time.Second
	routeBreakerCloseSuccesses = 3
	routeHalfOpenWeight        = 0.3
	routeWorstHealth           = 2
	routeEvictionLogInterval   = time.Minute
)

// routeObservationCapacity 是观察表和熔断表各自的键数上限；变量形式只为测试能调小。
var routeObservationCapacity = 10000

type routeBreakerKey struct {
	id    int
	model string
}

type routeFailureBucket struct {
	tick            int64
	total, failures uint32
}

// Why a breaker opened, reported in the Root-only routing snapshot.
const (
	routeTriggerConsecutive = "consecutive"
	routeTriggerWindowRate  = "window_rate"
	routeTriggerProbeFailed = "probe_failed"
)

// openUntil 为 0 表示闭合；非 0 时 openUntil 之前是打开，之后是半开。trigger、openedAt
// 和 reopens 描述当前这次打开，闭合时随状态一起清空；lastStatus 和 lastErrorCode 是最近
// 一次失败的状态码与错误码，不含上游消息。
type routeBreakerState struct {
	failures, successes  int
	openUntil, updatedAt int64
	probe                *routeProbe
	window               [6]routeFailureBucket
	trigger              string
	openedAt             int64
	reopens              int
	lastStatus           int
	lastErrorCode        string
}

// routeBreakerSample is one attempt counted by the breaker; status and code
// describe a failure.
type routeBreakerSample struct {
	success bool
	status  int
	code    string
}

// routeCooldown is a channel's 429 cooldown; source is retry_after or headerless.
type routeCooldown struct {
	until  int64
	source string
}

// 每个许可是唯一对象，旧许可的完成或释放不会影响其它探测或其它配置代际。
type routeProbe struct {
	key   routeBreakerKey
	epoch uint64
}

// count adds one sample and returns the trigger when the breaker must open.
func (s *routeBreakerState) count(now int64, success bool) string {
	tick := now / routeBreakerBucketDuration.Milliseconds()
	bucket := &s.window[tick%int64(len(s.window))]
	if bucket.tick != tick {
		*bucket = routeFailureBucket{tick: tick}
	}
	bucket.total++
	if success {
		s.failures = 0
	} else {
		s.failures++
		bucket.failures++
	}
	var total, failed uint64
	for _, b := range s.window {
		if b.tick > tick-int64(len(s.window)) {
			total += uint64(b.total)
			failed += uint64(b.failures)
		}
	}
	switch {
	case s.failures >= routeBreakerOpenFailures:
		return routeTriggerConsecutive
	case total >= routeBreakerWindowSamples && 2*failed >= total:
		return routeTriggerWindowRate
	}
	return ""
}

func (s *routeBreakerState) noteFailure(sample routeBreakerSample) {
	if sample.success {
		return
	}
	s.lastStatus, s.lastErrorCode = sample.status, routeErrorCode(sample.code)
}

func (s *routeObservationStore) recordBreaker(epoch uint64, key routeBreakerKey, probe *routeProbe, sample routeBreakerSample) {
	if key.id <= 0 || key.model == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	if s.staleLocked(epoch, key.id) {
		return
	}
	s.pruneLocked(now)
	state, present := s.breakers[key]
	if present && state.openUntil == 0 && now-state.updatedAt > routeObservationTTL.Milliseconds() {
		state = routeBreakerState{}
	}
	if state.openUntil != 0 {
		if probe == nil || probe != state.probe || probe.epoch != epoch {
			return
		}
		state.probe = nil
		if sample.success {
			state.successes++
			if state.successes == routeBreakerCloseSuccesses {
				state = routeBreakerState{}
				state.count(now, true)
			}
		} else {
			state = routeBreakerState{openUntil: now + routeBreakerCooldown.Milliseconds(),
				trigger: routeTriggerProbeFailed, openedAt: now, reopens: state.reopens + 1}
			state.noteFailure(sample)
		}
	} else {
		if probe != nil {
			return
		}
		if !present && len(s.breakers) >= routeObservationCapacity && !s.makeBreakerRoomLocked(now) {
			return
		}
		state.noteFailure(sample)
		if trigger := state.count(now, sample.success); trigger != "" {
			state.openUntil = now + routeBreakerCooldown.Milliseconds()
			state.trigger, state.openedAt = trigger, now
		}
	}
	state.updatedAt = now
	s.breakers[key] = state
}

// 表满时只淘汰闭合渠道的样本。撤掉仍在生效的熔断会重新放入故障流量。
func (s *routeObservationStore) makeBreakerRoomLocked(now int64) bool {
	var oldest routeBreakerKey
	oldestAt := int64(math.MaxInt64)
	for k, v := range s.breakers {
		if v.openUntil == 0 && v.updatedAt < oldestAt {
			oldest, oldestAt = k, v.updatedAt
		}
	}
	if oldestAt != math.MaxInt64 {
		delete(s.breakers, oldest)
	}
	s.logEvictionLocked(now)
	return len(s.breakers) < routeObservationCapacity
}

type routeAvailability int

const (
	routeAvailable routeAvailability = iota
	routeCooling
	routeOpen
	routeProbeBusy
)

// 必须先判断熔断，以免冷却兜底放行一个同时处于熔断中的渠道。
func (s *routeObservationStore) availabilityLocked(key routeBreakerKey, now int64) (routeAvailability, int64) {
	state := s.breakers[key]
	switch {
	case now < state.openUntil:
		return routeOpen, state.openUntil
	case state.probe != nil:
		return routeProbeBusy, 0
	case now < s.cooldowns[key.id].until:
		return routeCooling, s.cooldowns[key.id].until
	default:
		return routeAvailable, 0
	}
}

func (s *routeObservationStore) availability(key routeBreakerKey) (routeAvailability, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.availabilityLocked(key, time.Now().UnixMilli())
}

func (s *routeObservationStore) routeHealth(channelID int, model string) (int, float64) {
	key := routeBreakerKey{channelID, model}
	s.mu.Lock()
	defer s.mu.Unlock()
	class, _ := s.availabilityLocked(key, time.Now().UnixMilli())
	if class != routeAvailable {
		return routeWorstHealth, 1
	}
	if s.breakers[key].openUntil != 0 {
		return 0, routeHalfOpenWeight
	}
	return 0, 1
}

func (s *routeObservationStore) claimProbe(key routeBreakerKey, epoch uint64, allowCooling bool, preview bool) (*routeProbe, routeAvailability, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	class, until := s.availabilityLocked(key, time.Now().UnixMilli())
	if class != routeAvailable && !(class == routeCooling && allowCooling) {
		return nil, class, until
	}
	state := s.breakers[key]
	if state.openUntil == 0 || preview {
		return nil, routeAvailable, 0
	}
	if s.staleLocked(epoch, key.id) {
		return nil, routeProbeBusy, 0
	}
	state.probe = &routeProbe{key: key, epoch: epoch}
	s.breakers[key] = state
	return state.probe, routeAvailable, 0
}

func (s *routeObservationStore) releaseProbe(probe *routeProbe) {
	if probe == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.breakers[probe.key]
	if state.probe == probe {
		state.probe = nil
		s.breakers[probe.key] = state
	}
}

type RouteBreakerSnapshot struct {
	ChannelID     int    `json:"channel_id"`
	Model         string `json:"model"`
	State         string `json:"state"`
	OpenUntil     int64  `json:"open_until"`
	ProbeInFlight bool   `json:"probe_in_flight"`
	Trigger       string `json:"trigger"`
	LastStatus    int    `json:"last_status,omitempty"`
	LastErrorCode string `json:"last_error_code,omitempty"`
	OpenedAt      int64  `json:"opened_at"`
	Reopens       int    `json:"reopens"`
}
type RouteCooldownSnapshot struct {
	ChannelID int    `json:"channel_id"`
	Until     int64  `json:"until"`
	Source    string `json:"source"`
}

func (s *routeObservationStore) healthSnapshot() ([]RouteBreakerSnapshot, []RouteCooldownSnapshot) {
	now := time.Now().UnixMilli()
	breakers := []RouteBreakerSnapshot{}
	cooldowns := []RouteCooldownSnapshot{}
	s.mu.Lock()
	for key, state := range s.breakers {
		if state.openUntil == 0 {
			continue
		}
		label := "open"
		if now >= state.openUntil {
			label = "half_open"
		}
		breakers = append(breakers, RouteBreakerSnapshot{ChannelID: key.id, Model: key.model, State: label, OpenUntil: state.openUntil,
			ProbeInFlight: state.probe != nil, Trigger: state.trigger, LastStatus: state.lastStatus, LastErrorCode: state.lastErrorCode,
			OpenedAt: state.openedAt, Reopens: state.reopens})
	}
	for id, cooldown := range s.cooldowns {
		if cooldown.until > now {
			cooldowns = append(cooldowns, RouteCooldownSnapshot{ChannelID: id, Until: cooldown.until, Source: cooldown.source})
		}
	}
	s.mu.Unlock()
	sort.Slice(breakers, func(i, j int) bool {
		if breakers[i].ChannelID != breakers[j].ChannelID {
			return breakers[i].ChannelID < breakers[j].ChannelID
		}
		return breakers[i].Model < breakers[j].Model
	})
	sort.Slice(cooldowns, func(i, j int) bool { return cooldowns[i].ChannelID < cooldowns[j].ChannelID })
	return breakers, cooldowns
}

// coolDown 只延长不缩短渠道的 429 冷却；来源随生效的那次冷却。
func (s *routeObservationStore) coolDown(epoch uint64, channelID int, cooldown routeCooldown) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if channelID <= 0 || s.staleLocked(epoch, channelID) || cooldown.until <= s.cooldowns[channelID].until {
		return
	}
	if _, ok := s.cooldowns[channelID]; !ok && len(s.cooldowns) >= routeObservationCapacity {
		now := time.Now().UnixMilli()
		for id, end := range s.cooldowns {
			if end.until <= now {
				delete(s.cooldowns, id)
			}
		}
		if len(s.cooldowns) >= routeObservationCapacity {
			s.logEvictionLocked(now)
			return
		}
	}
	s.cooldowns[channelID] = cooldown
}

// retryAfterUntil 计算上游 429 的冷却。缺头使用短冷却，显式值仅接受有效的等待期。
func retryAfterUntil(header http.Header, now time.Time) (routeCooldown, bool) {
	values, present := header["Retry-After"]
	if !present {
		return routeCooldown{now.Add(routeHeaderless429Cooldown).UnixMilli(), "headerless"}, true
	}
	if len(values) == 0 {
		return routeCooldown{}, false
	}
	value := strings.TrimSpace(values[0])
	var wait time.Duration
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return routeCooldown{}, false
		}
		wait = time.Duration(min(seconds, int64(routeObservationTTL/time.Second))) * time.Second
	} else if at, err := http.ParseTime(value); err == nil {
		wait = at.Sub(now)
	} else {
		return routeCooldown{}, false
	}
	if wait <= 0 {
		return routeCooldown{}, false
	}
	return routeCooldown{now.Add(min(wait, routeObservationTTL)).UnixMilli(), "retry_after"}, true
}

// evictForInsertLocked 在表满时为一个新键腾位置：先删过期项，仍满就删最旧的一项，
// 淘汰按 routeEvictionLogInterval 限频记日志。调用方持有 s.mu。
func evictForInsertLocked[K comparable, V any](s *routeObservationStore, rows map[K]V, updatedAt func(V) int64, now int64) {
	if len(rows) < routeObservationCapacity {
		return
	}
	ttl := routeObservationTTL.Milliseconds()
	var oldest K
	oldestAt, found := int64(math.MaxInt64), false
	for key, row := range rows {
		at := updatedAt(row)
		if now-at > ttl {
			delete(rows, key)
			continue
		}
		if at < oldestAt {
			oldest, oldestAt, found = key, at, true
		}
	}
	if len(rows) < routeObservationCapacity || !found {
		return
	}
	delete(rows, oldest)
	s.logEvictionLocked(now)
}

func (s *routeObservationStore) logEvictionLocked(now int64) {
	s.evicted++
	if now-s.lastEvictionLog >= routeEvictionLogInterval.Milliseconds() {
		common.SysLog(common.LogText("route observation store reached %d keys: evicted or skipped %d entries since the last report", routeObservationCapacity, s.evicted))
		s.evicted, s.lastEvictionLog = 0, now
	}
}
