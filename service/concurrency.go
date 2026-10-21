package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var ErrConcurrencyFull = errors.New("concurrency limit reached")

var errConcurrencyPublicationPending = errors.New("concurrency limit publication pending")

type concurrencyKey struct {
	kind string
	id   int
}

// retired 表示渠道已删除但仍有在途许可。同 ID 再次准入时沿用这份计数并按新渠道的上限
// 重新设定，最后一个许可释放后移除表项。
type concurrencyResource struct {
	limit, active int
	retired       bool
}

// Resource identities survive limit changes, including changes to and from unlimited.
type ConcurrencyManager struct {
	mu        sync.Mutex
	resources map[concurrencyKey]*concurrencyResource
	// An unconfirmed publication blocks this resource's new admission until an
	// authoritative database read succeeds. Reads use only the short state lock.
	pending     map[concurrencyKey]uint64
	publication uint64
}

type ConcurrencyPermit struct {
	once     sync.Once
	manager  *ConcurrencyManager
	key      concurrencyKey
	resource *concurrencyResource
}

type ConcurrencySnapshot struct {
	ID       int `json:"id"`
	Limit    int `json:"limit"`
	InFlight int `json:"in_flight"`
}

func NewConcurrencyManager() *ConcurrencyManager {
	return &ConcurrencyManager{resources: make(map[concurrencyKey]*concurrencyResource), pending: make(map[concurrencyKey]uint64)}
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
	if m.pending[key] != 0 {
		return nil, errConcurrencyPublicationPending
	}
	r := m.resources[key]
	if r == nil {
		r = &concurrencyResource{limit: initialLimit}
		m.resources[key] = r
	}
	if r.retired {
		r.limit, r.retired = initialLimit, false
	}
	if r.limit > 0 && r.active >= r.limit {
		return nil, ErrConcurrencyFull
	}
	r.active++
	return &ConcurrencyPermit{manager: m, key: key, resource: r}, nil
}

func (p *ConcurrencyPermit) Release() {
	if p == nil {
		return
	}
	p.once.Do(func() { p.manager.release(p.key, p.resource) })
}

func (m *ConcurrencyManager) release(key concurrencyKey, r *concurrencyResource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.active--
	if r.retired && r.active == 0 && m.resources[key] == r {
		delete(m.resources, key)
	}
}

func AcquireUserConcurrency(ctx context.Context, userID, limit int) (*ConcurrencyPermit, error) {
	return acquireConcurrency(ctx, concurrencyKey{"user", userID}, limit)
}

func AcquireChannelConcurrency(ctx context.Context, channel *model.Channel) (*ConcurrencyPermit, error) {
	if channel == nil {
		return nil, errors.New("missing channel")
	}
	return acquireConcurrency(ctx, concurrencyKey{"channel", channel.Id}, channel.MaxConcurrency)
}

func acquireConcurrency(ctx context.Context, key concurrencyKey, initialLimit int) (*ConcurrencyPermit, error) {
	for {
		permit, err := gatewayConcurrency.tryAcquire(ctx, key.kind, key.id, initialLimit)
		if !errors.Is(err, errConcurrencyPublicationPending) {
			return permit, err
		}
		if err := refreshPendingConcurrencyLimit(ctx, key); err != nil {
			return nil, err
		}
	}
}

func ChannelInFlight(id int) int {
	gatewayConcurrency.mu.Lock()
	defer gatewayConcurrency.mu.Unlock()
	if r := gatewayConcurrency.resources[concurrencyKey{"channel", id}]; r != nil {
		return r.active
	}
	return 0
}

// ReleaseDeletedChannels 是渠道删除后的共享清理：删除指向已不存在渠道的 Key 路由条目，
// 移除这些渠道的并发资源表项，并让观察、熔断和冷却失效，使删除前发出的尝试不能写进
// 复用同一 ID 的新渠道。渠道删除已经提交，清理失败只记日志，残留条目不参与选路，
// 会在下一次删除渠道时被清扫。
func ReleaseDeletedChannels(ctx context.Context, channelIDs []int) {
	if err := model.DeleteOrphanChannelRouteEntries(context.WithoutCancel(ctx)); err != nil {
		common.SysError(common.LogText("failed to delete route entries of deleted channels: %v", err))
	}
	gatewayConcurrency.forget("channel", channelIDs)
	for _, id := range channelIDs {
		routeObservations.invalidate(id)
	}
}

// forget 移除空闲的资源表项；仍有在途许可的表项标记为 retired，同 ID 再准入时继续共用
// 计数，最后一个许可释放时移除。
func (m *ConcurrencyManager) forget(kind string, ids []int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		key := concurrencyKey{kind, id}
		delete(m.pending, key)
		if r := m.resources[key]; r != nil && r.active > 0 {
			r.retired = true
			continue
		}
		delete(m.resources, key)
	}
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
	return publishConcurrencyLimit(ctx, concurrencyKey{"user", id})
}
func RefreshChannelConcurrencyLimit(ctx context.Context, id int) error {
	return publishConcurrencyLimit(ctx, concurrencyKey{"channel", id})
}

func publishConcurrencyLimit(ctx context.Context, key concurrencyKey) error {
	// A committed setting must reach admission even when its HTTP caller has
	// disconnected. Failed reads remain pending and admission retries them.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	gatewayConcurrency.mu.Lock()
	gatewayConcurrency.publication++
	gatewayConcurrency.pending[key] = gatewayConcurrency.publication
	gatewayConcurrency.mu.Unlock()
	concurrencyPublishMu.Lock()
	defer concurrencyPublishMu.Unlock()
	return refreshConcurrencyLimitLocked(ctx, key)
}

func refreshPendingConcurrencyLimit(ctx context.Context, key concurrencyKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !gatewayConcurrency.publicationPending(key) {
		return nil
	}
	concurrencyPublishMu.Lock()
	defer concurrencyPublishMu.Unlock()
	if !gatewayConcurrency.publicationPending(key) {
		return nil
	}
	return refreshConcurrencyLimitLocked(ctx, key)
}

func (m *ConcurrencyManager) publicationPending(key concurrencyKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending[key] != 0
}

func refreshConcurrencyLimitLocked(ctx context.Context, key concurrencyKey) error {
	gatewayConcurrency.mu.Lock()
	publication := gatewayConcurrency.pending[key]
	gatewayConcurrency.mu.Unlock()
	var limit int
	var err error
	if key.kind == "user" {
		var row model.User
		err = model.DB.WithContext(ctx).Select("id", "max_concurrency").First(&row, key.id).Error
		limit = row.MaxConcurrency
	} else {
		var row model.Channel
		err = model.DB.WithContext(ctx).Select("id", "max_concurrency").First(&row, key.id).Error
		limit = row.MaxConcurrency
	}
	if err != nil {
		return err
	}
	if limit < 0 {
		return errors.New("invalid concurrency limit")
	}
	gatewayConcurrency.setLimit(key.kind, key.id, limit, false)
	gatewayConcurrency.mu.Lock()
	if gatewayConcurrency.pending[key] == publication {
		delete(gatewayConcurrency.pending, key)
	}
	gatewayConcurrency.mu.Unlock()
	if key.kind == "channel" {
		routeObservations.invalidate(key.id)
	}
	return nil
}
func init() {
	model.ChannelRoutingChanged = func(id int) error { return RefreshChannelConcurrencyLimit(context.Background(), id) }
}
