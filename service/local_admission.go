package service

import (
	"container/list"
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"time"
)

var ErrGatewayDraining = errors.New("gateway is draining")

type AdmissionPolicy struct {
	Capacity      int
	QueueSize     int
	Revision      uint64
	ConfigVersion int64
}

type AdmissionSnapshot struct {
	InFlight      int64  `json:"in_flight"`
	Queued        int64  `json:"queued"`
	Capacity      int    `json:"capacity"`
	Revision      uint64 `json:"revision"`
	ConfigVersion int64  `json:"config_version"`
	StartedAt     int64  `json:"started_at"`
	Draining      bool   `json:"draining"`
}

type admissionWaiter struct {
	started time.Time
	ctx     context.Context
	done    chan struct{}
	permit  *Permit
	err     error
	element *list.Element
}

type admissionPool struct {
	key     string
	policy  AdmissionPolicy
	active  int64
	waiters list.List
	invalid bool
}

// AdmissionManager keeps resource identity stable across configuration changes.
// The short state lock also makes pool reclamation and total queue accounting
// atomic; no external I/O or callbacks run while it is held.
type AdmissionManager struct {
	mu             sync.Mutex
	pools          map[string]*admissionPool
	totalQueued    int
	totalLimit     int
	draining       bool
	startedAt      int64
	granted        uint64
	released       uint64
	queueFull      uint64
	totalQueueFull uint64
	timeouts       uint64
	canceled       uint64
	waitBuckets    [8]uint64
}

type Permit struct {
	once      sync.Once
	manager   *AdmissionManager
	pool      *admissionPool
	key       string
	ChannelID int
}

func NewAdmissionManager(totalLimit int) *AdmissionManager {
	return &AdmissionManager{pools: make(map[string]*admissionPool), totalLimit: totalLimit, startedAt: time.Now().Unix()}
}

var gatewayAdmission = NewAdmissionManager(1024)

type AdmissionMetrics struct {
	InFlight       int64     `json:"in_flight"`
	Queued         int       `json:"queued"`
	TotalLimit     int       `json:"total_queue_size"`
	Pools          int       `json:"pools"`
	Granted        uint64    `json:"granted_total"`
	Released       uint64    `json:"released_total"`
	Draining       bool      `json:"draining"`
	QueueFull      uint64    `json:"queue_full_total"`
	TotalQueueFull uint64    `json:"total_queue_full_total"`
	Timeouts       uint64    `json:"wait_timeout_total"`
	Canceled       uint64    `json:"canceled_total"`
	WaitBuckets    [8]uint64 `json:"wait_duration_ms_buckets"`
}

func (m *AdmissionManager) Metrics() AdmissionMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := AdmissionMetrics{Queued: m.totalQueued, TotalLimit: m.totalLimit, Pools: len(m.pools), Granted: m.granted, Released: m.released, Draining: m.draining}
	s.QueueFull, s.TotalQueueFull, s.Timeouts, s.Canceled, s.WaitBuckets = m.queueFull, m.totalQueueFull, m.timeouts, m.canceled, m.waitBuckets
	for _, pool := range m.pools {
		s.InFlight += pool.active
	}
	return s
}

func (permit *Permit) Release() {
	if permit == nil {
		return
	}
	permit.once.Do(func() {
		m := permit.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		permit.pool.active--
		m.released++
		m.dispatch(permit.pool)
		m.reclaim(permit.key, permit.pool)
	})
}

func (m *AdmissionManager) reclaim(key string, pool *admissionPool) {
	if pool.active == 0 && pool.waiters.Len() == 0 && m.pools[key] == pool {
		delete(m.pools, key)
	}
}

func (m *AdmissionManager) grant(key string, pool *admissionPool) *Permit {
	pool.active++
	m.granted++
	return &Permit{manager: m, pool: pool, key: key}
}

func (m *AdmissionManager) dispatch(pool *admissionPool) {
	if m.draining && strings.HasPrefix(pool.key, "user:") {
		return
	}
	for !pool.invalid && pool.waiters.Len() > 0 && (pool.policy.Capacity == 0 || pool.active < int64(pool.policy.Capacity)) {
		w := pool.waiters.Front().Value.(*admissionWaiter)
		pool.waiters.Remove(w.element)
		w.element = nil
		m.totalQueued--
		if err := w.ctx.Err(); err != nil {
			w.err = err
		} else {
			pool.active++
			m.granted++
		}
		m.recordWait(pool.key, w)
		close(w.done)
	}
}

func (m *AdmissionManager) ApplyPolicy(key string, policy AdmissionPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool := m.pools[key]; pool != nil && policy.Revision >= pool.policy.Revision {
		pool.policy, pool.invalid = policy, false
		m.dispatch(pool)
	}
}

func (m *AdmissionManager) SetTotalLimit(limit int) {
	m.mu.Lock()
	m.totalLimit = limit
	m.mu.Unlock()
}

// Invalidate fences grants until the committed configuration has been loaded.
func (m *AdmissionManager) Invalidate(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pool := m.pools[key]; pool != nil {
		pool.invalid = true
	}
}

func (m *AdmissionManager) Reject(key string, err error) {
	m.RejectRevision(key, ^uint64(0), err)
}

func (m *AdmissionManager) RejectRevision(key string, revision uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool := m.pools[key]
	if pool == nil {
		return
	}
	if revision < pool.policy.Revision {
		return
	}
	pool.invalid = true
	for pool.waiters.Len() > 0 {
		w := pool.waiters.Front().Value.(*admissionWaiter)
		pool.waiters.Remove(w.element)
		w.element, w.err = nil, err
		m.totalQueued--
		m.recordWait(key, w)
		close(w.done)
	}
	m.reclaim(key, pool)
}

func (m *AdmissionManager) TryAcquire(ctx context.Context, key string, policy AdmissionPolicy) (*Permit, error) {
	return m.Acquire(ctx, key, policy, false)
}

func (m *AdmissionManager) Acquire(ctx context.Context, key string, policy AdmissionPolicy, wait bool) (*Permit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" || policy.Capacity < 0 || policy.QueueSize < 0 {
		return nil, ErrAdmissionRuntime
	}
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if m == gatewayAdmission && policy.Revision != admissionRevision.Load() {
		m.mu.Unlock()
		return nil, ErrAdmissionRuntime
	}
	if m.draining && len(key) >= 5 && key[:5] == "user:" {
		m.mu.Unlock()
		return nil, ErrGatewayDraining
	}
	pool := m.pools[key]
	if pool == nil {
		pool = &admissionPool{key: key, policy: policy}
		m.pools[key] = pool
	}
	if policy.Revision >= pool.policy.Revision {
		pool.policy, pool.invalid = policy, false
		m.dispatch(pool)
	}
	if pool.invalid {
		m.mu.Unlock()
		return nil, ErrAdmissionRuntime
	}
	if pool.waiters.Len() == 0 && (pool.policy.Capacity == 0 || pool.active < int64(pool.policy.Capacity)) {
		permit := m.grant(key, pool)
		m.mu.Unlock()
		return permit, nil
	}
	if !wait {
		m.mu.Unlock()
		return nil, ErrAdmissionCapacity
	}
	if pool.waiters.Len() >= pool.policy.QueueSize {
		m.queueFull++
		m.mu.Unlock()
		return nil, ErrGatewayQueueFull
	}
	if m.totalQueued >= m.totalLimit {
		m.totalQueueFull++
		m.mu.Unlock()
		return nil, ErrGatewayTotalQueueFull
	}
	w := &admissionWaiter{ctx: ctx, started: time.Now(), done: make(chan struct{}), permit: &Permit{manager: m, pool: pool, key: key}}
	w.element = pool.waiters.PushBack(w)
	m.totalQueued++
	if m == gatewayAdmission {
		if counter := gatewayQueueCounter(key); counter != nil {
			counter.entered.Add(1)
			counter.active.Add(1)
		}
	}
	m.mu.Unlock()
	select {
	case <-w.done:
	case <-ctx.Done():
	}
	m.mu.Lock()
	if w.element != nil {
		pool.waiters.Remove(w.element)
		w.element, w.err = nil, ctx.Err()
		m.totalQueued--
		m.recordWait(key, w)
		m.dispatch(pool)
	}
	err := w.err
	m.reclaim(key, pool)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		w.permit.Release()
		return nil, err
	}
	return w.permit, nil
}

// Bounds are cumulative milliseconds: 1, 5, 20, 100, 500, 1000, 5000, +Inf.
func (m *AdmissionManager) recordWait(key string, waiter *admissionWaiter) {
	waited := time.Since(waiter.started).Milliseconds()
	for i, bound := range [...]int64{1, 5, 20, 100, 500, 1000, 5000, math.MaxInt64} {
		if waited <= bound {
			m.waitBuckets[i]++
		}
	}
	if errors.Is(waiter.err, context.Canceled) {
		m.canceled++
	}
	if errors.Is(waiter.err, context.DeadlineExceeded) {
		m.timeouts++
	}
	if m != gatewayAdmission {
		return
	}
	if counter := gatewayQueueCounter(key); counter != nil {
		counter.active.Add(-1)
		counter.waitMS.Add(waited)
		if waiter.err == nil {
			counter.completed.Add(1)
		} else {
			counter.failed.Add(1)
		}
	}
}

func (m *AdmissionManager) Snapshot(key string) AdmissionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := AdmissionSnapshot{StartedAt: m.startedAt, Draining: m.draining}
	if p := m.pools[key]; p != nil {
		s.InFlight, s.Queued, s.Capacity = p.active, int64(p.waiters.Len()), p.policy.Capacity
		s.Revision, s.ConfigVersion = p.policy.Revision, p.policy.ConfigVersion
	}
	return s
}

func (m *AdmissionManager) BeginDrain() {
	m.mu.Lock()
	m.draining = true
	keys := make([]string, 0)
	for key := range m.pools {
		if len(key) >= 5 && key[:5] == "user:" {
			keys = append(keys, key)
		}
	}
	m.mu.Unlock()
	for _, key := range keys {
		m.Reject(key, ErrGatewayDraining)
	}
}
