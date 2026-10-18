package service

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/model"
)

var ErrConcurrencyFull = errors.New("concurrency limit reached")

type concurrencyKey struct {
	kind string
	id   int
}
type concurrencyResource struct{ limit, active int }

// Resource identities survive limit changes, including changes to and from unlimited.
type ConcurrencyManager struct {
	mu        sync.Mutex
	resources map[concurrencyKey]*concurrencyResource
}

type ConcurrencyPermit struct {
	once     sync.Once
	manager  *ConcurrencyManager
	resource *concurrencyResource
}

type ConcurrencySnapshot struct {
	ID       int `json:"id"`
	Limit    int `json:"limit"`
	InFlight int `json:"in_flight"`
}

func NewConcurrencyManager() *ConcurrencyManager {
	return &ConcurrencyManager{resources: make(map[concurrencyKey]*concurrencyResource)}
}

var gatewayConcurrency = NewConcurrencyManager()

func (m *ConcurrencyManager) setLimit(kind string, id, limit int, initialize bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := concurrencyKey{kind, id}
	r := m.resources[key]
	if r == nil {
		r = &concurrencyResource{limit: limit}
		m.resources[key] = r
		return
	}
	if !initialize {
		r.limit = limit
	}
}

func (m *ConcurrencyManager) tryAcquire(ctx context.Context, kind string, id, initialLimit int) (*ConcurrencyPermit, error) {
	if id <= 0 || initialLimit < 0 {
		return nil, errors.New("invalid concurrency resource")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := concurrencyKey{kind, id}
	r := m.resources[key]
	if r == nil {
		r = &concurrencyResource{limit: initialLimit}
		m.resources[key] = r
	}
	if r.limit > 0 && r.active >= r.limit {
		return nil, ErrConcurrencyFull
	}
	r.active++
	return &ConcurrencyPermit{manager: m, resource: r}, nil
}

func (p *ConcurrencyPermit) Release() {
	if p == nil {
		return
	}
	p.once.Do(func() { p.manager.mu.Lock(); defer p.manager.mu.Unlock(); p.resource.active-- })
}

func AcquireUserConcurrency(ctx context.Context, userID, limit int) (*ConcurrencyPermit, error) {
	return gatewayConcurrency.tryAcquire(ctx, "user", userID, limit)
}

func AcquireChannelConcurrency(ctx context.Context, channel *model.Channel) (*ConcurrencyPermit, error) {
	if channel == nil {
		return nil, errors.New("missing channel")
	}
	return gatewayConcurrency.tryAcquire(ctx, "channel", channel.Id, channel.MaxConcurrency)
}

func ChannelInFlight(id int) int {
	gatewayConcurrency.mu.Lock()
	defer gatewayConcurrency.mu.Unlock()
	if r := gatewayConcurrency.resources[concurrencyKey{"channel", id}]; r != nil {
		return r.active
	}
	return 0
}

func (m *ConcurrencyManager) snapshot(kind string) []ConcurrencySnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := make([]ConcurrencySnapshot, 0)
	for key, r := range m.resources {
		if key.kind == kind {
			rows = append(rows, ConcurrencySnapshot{key.id, r.limit, r.active})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

type RoutingRuntimeSnapshot struct {
	Users        []ConcurrencySnapshot      `json:"users"`
	Channels     []ConcurrencySnapshot      `json:"channels"`
	Observations []RouteObservationSnapshot `json:"observations"`
}

func RoutingSnapshot() RoutingRuntimeSnapshot {
	return RoutingRuntimeSnapshot{gatewayConcurrency.snapshot("user"), gatewayConcurrency.snapshot("channel"), routeObservations.snapshot()}
}

// Publication serializes a committed database read with publication. Concurrent
// administrators cannot publish stale request payloads after a newer commit.
var concurrencyPublishMu sync.Mutex

func RefreshUserConcurrencyLimit(ctx context.Context, id int) error {
	concurrencyPublishMu.Lock()
	defer concurrencyPublishMu.Unlock()
	var row model.User
	if err := model.DB.WithContext(ctx).Select("id", "max_concurrency").First(&row, id).Error; err != nil {
		return err
	}
	if row.MaxConcurrency < 0 {
		return errors.New("invalid user concurrency limit")
	}
	gatewayConcurrency.setLimit("user", id, row.MaxConcurrency, false)
	return nil
}
func RefreshChannelConcurrencyLimit(ctx context.Context, id int) error {
	concurrencyPublishMu.Lock()
	defer concurrencyPublishMu.Unlock()
	var row model.Channel
	if err := model.DB.WithContext(ctx).Select("id", "max_concurrency").First(&row, id).Error; err != nil {
		return err
	}
	if row.MaxConcurrency < 0 {
		return errors.New("invalid channel concurrency limit")
	}
	gatewayConcurrency.setLimit("channel", id, row.MaxConcurrency, false)
	routeObservations.invalidate(id)
	return nil
}
func init() {
	model.ChannelRuntimeOccupancy = ChannelInFlight
	model.ChannelRoutingChanged = func(id int) error { return RefreshChannelConcurrencyLimit(context.Background(), id) }
}
