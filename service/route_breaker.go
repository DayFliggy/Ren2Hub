package service

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 熔断按“渠道×实际模型”计数，只影响自动排序的健康档：打开时降到最差档，半开时权重乘
// routeHalfOpenWeight。渠道从不因此被剔除，手动列表也不读它。状态只在进程内，沿用观察表的
// 过期时间和容量上限。做法借鉴 AxonHub 的模型熔断，没有搬代码。
const (
	routeBreakerOpenFailures   = 5
	routeBreakerCooldown       = time.Minute
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

// openUntil 为 0 表示闭合；非 0 时 openUntil 之前是打开，之后是半开。
type routeBreakerState struct {
	failures, successes  int
	openUntil, updatedAt int64
}

// recordRouteBreaker 记录一次真实发出的尝试。epoch 规则与观察样本相同：配置变更后，
// 旧选择的结果不能写进新配置。
func recordRouteBreaker(epoch uint64, channelID int, model string, success bool) {
	if channelID <= 0 || model == "" {
		return
	}
	s := &routeObservations
	now := time.Now().UnixMilli()
	key := routeBreakerKey{channelID, model}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed[channelID] > epoch {
		return
	}
	s.pruneLocked(now)
	state, present := s.breakers[key]
	if present && now-state.updatedAt > routeObservationTTL.Milliseconds() {
		state = routeBreakerState{}
	}
	if now < state.openUntil {
		// 打开期间不计数：冷却按时结束，既不提前闭合，也不被持续失败无限延长。
		return
	}
	switch {
	case success && state.openUntil == 0:
		delete(s.breakers, key)
		return
	case success:
		state.successes++
		if state.successes >= routeBreakerCloseSuccesses {
			delete(s.breakers, key)
			return
		}
	case state.openUntil != 0:
		state = routeBreakerState{openUntil: now + routeBreakerCooldown.Milliseconds()}
	default:
		state.failures++
		if state.failures >= routeBreakerOpenFailures {
			state.openUntil = now + routeBreakerCooldown.Milliseconds()
		}
	}
	if !present {
		evictForInsertLocked(s, s.breakers, func(v routeBreakerState) int64 { return v.updatedAt }, now)
	}
	state.updatedAt = now
	s.breakers[key] = state
}

// routeHealth 返回自动排序用的健康档和权重系数。model 为空时只看渠道级的 429 冷却。
func (s *routeObservationStore) routeHealth(channelID int, model string) (int, float64) {
	now := time.Now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	if now < s.cooldowns[channelID] {
		return routeWorstHealth, 1
	}
	state, ok := s.breakers[routeBreakerKey{channelID, model}]
	if !ok || state.openUntil == 0 || now-state.updatedAt > routeObservationTTL.Milliseconds() {
		return 0, 1
	}
	if now < state.openUntil {
		return routeWorstHealth, 1
	}
	return 0, routeHalfOpenWeight
}

// coolDown 只延长不缩短渠道的 429 冷却。
func (s *routeObservationStore) coolDown(epoch uint64, channelID int, until int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if channelID <= 0 || s.changed[channelID] > epoch || until <= s.cooldowns[channelID] {
		return
	}
	s.cooldowns[channelID] = until
}

// retryAfterUntil 解析上游 429 的 Retry-After（秒数或 HTTP 日期），上限为观察表的过期时间。
// 缺失、无法解析或已过去的值不产生冷却。
func retryAfterUntil(header http.Header, now time.Time) (int64, bool) {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0, false
	}
	var wait time.Duration
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		wait = time.Duration(min(seconds, int64(routeObservationTTL/time.Second))) * time.Second
	} else if at, err := http.ParseTime(value); err == nil {
		wait = at.Sub(now)
	} else {
		return 0, false
	}
	if wait <= 0 {
		return 0, false
	}
	return now.Add(min(wait, routeObservationTTL)).UnixMilli(), true
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
	s.evicted++
	if now-s.lastEvictionLog >= routeEvictionLogInterval.Milliseconds() {
		common.SysLog(common.LogText("route observation store reached %d keys: evicted %d oldest entries since the last report", routeObservationCapacity, s.evicted))
		s.evicted, s.lastEvictionLog = 0, now
	}
}
