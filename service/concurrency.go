package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"gorm.io/gorm"
)

var ErrConcurrencyFull = errors.New("concurrency limit reached")

var errConcurrencyPublicationPending = errors.New("concurrency limit publication pending")

// errConcurrencyLimitUnavailable 表示读不到有效的并发上限，准入无法判定。它不是满载，
// 也不是资源已删除；所有入口都按 503 <kind>_concurrency_unavailable 立即返回。
var errConcurrencyLimitUnavailable = errors.New("concurrency limit unavailable")

// errConcurrencyResourceDeleted 表示删除留下的待确认标记仍在：数据库里没有这个渠道或用户，
// 删除前取得旧数据的请求不能再准入，也不能重新建立表项。
var errConcurrencyResourceDeleted = errors.New("concurrency resource deleted")

type concurrencyKey struct {
	kind string
	id   int
}

// retired 表示渠道或用户已删除但仍有在途许可，最后一个许可释放后移除表项。同 ID 的记录
// 经数据库读取确认重新存在时清除 retired，新资源沿用这份计数。
type concurrencyResource struct {
	limit, active int
	retired       bool
}

// Resource identities survive limit changes, including changes to and from unlimited.
type ConcurrencyManager struct {
	mu            sync.Mutex
	resources     map[concurrencyKey]*concurrencyResource
	userActive    atomic.Int64
	channelActive atomic.Int64
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
	ID       int    `json:"id"`
	Name     string `json:"name,omitempty"`
	Limit    int    `json:"limit"`
	InFlight int    `json:"in_flight"`
}

func NewConcurrencyManager() *ConcurrencyManager {
	return &ConcurrencyManager{resources: make(map[concurrencyKey]*concurrencyResource), pending: make(map[concurrencyKey]uint64)}
}

var gatewayConcurrency = NewConcurrencyManager()

// setLimit 只在数据库读取成功后调用，读到记录即确认资源存在。
func (m *ConcurrencyManager) setLimit(kind string, id, limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := concurrencyKey{kind, id}
	r := m.resources[key]
	if r == nil {
		m.resources[key] = &concurrencyResource{limit: limit}
		return
	}
	r.limit, r.retired = limit, false
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
	if r.limit > 0 && r.active >= r.limit {
		return nil, ErrConcurrencyFull
	}
	r.active++
	m.addActive(kind, 1)
	return &ConcurrencyPermit{manager: m, key: key, resource: r}, nil
}

func (m *ConcurrencyManager) addActive(kind string, delta int64) {
	switch kind {
	case "user":
		m.userActive.Add(delta)
	case "channel":
		m.channelActive.Add(delta)
	}
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
	m.addActive(key.kind, -1)
	if r.retired && r.active == 0 && m.resources[key] == r {
		delete(m.resources, key)
	}
}

// ConcurrencyAdmissionError 是并发准入失败的唯一状态映射，kind 为 "user" 或 "channel"：满载 429；
// 资源已删除 403；读取上限等其他失败 503。
func ConcurrencyAdmissionError(kind string, err error) (int, types.ErrorCode) {
	switch {
	case errors.Is(err, ErrConcurrencyFull):
		return http.StatusTooManyRequests, types.ErrorCode(kind + "_concurrency_exceeded")
	case errors.Is(err, errConcurrencyResourceDeleted):
		return http.StatusForbidden, types.ErrorCode(kind + "_unavailable")
	default:
		return http.StatusServiceUnavailable, types.ErrorCode(kind + "_concurrency_unavailable")
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
	// 与上限发布串行：删除前读到的旧上限不能在清理之后写回，被复用同一 ID 的新渠道继承。
	concurrencyPublishMu.Lock()
	defer concurrencyPublishMu.Unlock()
	gatewayConcurrency.forget("channel", channelIDs)
	for _, id := range channelIDs {
		routeObservations.invalidate(id)
		routeObservations.clearDeletedChannelPending(id)
	}
}

// ReleaseDeletedUser 是硬删除用户后的并发清理，规则与 ReleaseDeletedChannels 相同：空闲表项
// 移除，仍有在途许可的标记为 retired，并与上限发布串行。
func ReleaseDeletedUser(userID int) {
	concurrencyPublishMu.Lock()
	defer concurrencyPublishMu.Unlock()
	gatewayConcurrency.forget("user", []int{userID})
}

// forget 移除空闲的资源表项；仍有在途许可的表项标记为 retired，最后一个许可释放时移除。
// 每个 ID 留下待确认的发布标记，之后的准入先读数据库：记录不存在就拒绝，删除前取得旧数据
// 的请求不能复活或重建表项；同 ID 的记录重新存在时读取成功，标记随之清除。标记数量以
// 删除的渠道和用户数为上限。
func (m *ConcurrencyManager) forget(kind string, ids []int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		key := concurrencyKey{kind, id}
		m.publication++
		m.pending[key] = m.publication
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
			rows = append(rows, ConcurrencySnapshot{ID: key.id, Limit: r.limit, InFlight: r.active})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func (m *ConcurrencyManager) totalInFlight() (users, channels int) {
	return int(m.userActive.Load()), int(m.channelActive.Load())
}

func gatewayInFlightTotals() (users, channels int) {
	return gatewayConcurrency.totalInFlight()
}

const routingNameLookupBatchSize = 500

// EnrichRoutingSnapshotNames fills display names for users and channels in a
// Root routing snapshot using bounded read-only batch queries outside the
// hot concurrency lock. Lookup errors are logged and leave Name empty rather
// than blocking the observability snapshot.
func EnrichRoutingSnapshotNames(ctx context.Context, snap *RoutingRuntimeSnapshot) {
	if snap == nil || model.DB == nil {
		return
	}
	userIDSet := make(map[int]struct{}, len(snap.Users))
	for _, row := range snap.Users {
		if row.ID > 0 {
			userIDSet[row.ID] = struct{}{}
		}
	}
	channelIDSet := make(map[int]struct{}, len(snap.Channels)+len(snap.Breakers)+len(snap.Cooldowns))
	for _, row := range snap.Channels {
		if row.ID > 0 {
			channelIDSet[row.ID] = struct{}{}
		}
	}
	for _, row := range snap.Breakers {
		if row.ChannelID > 0 {
			channelIDSet[row.ChannelID] = struct{}{}
		}
	}
	for _, row := range snap.Cooldowns {
		if row.ChannelID > 0 {
			channelIDSet[row.ChannelID] = struct{}{}
		}
	}
	if len(userIDSet) == 0 && len(channelIDSet) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if len(userIDSet) > 0 {
		names := lookupRoutingUserNames(queryCtx, sortedMapKeys(userIDSet))
		for i := range snap.Users {
			if name := names[snap.Users[i].ID]; name != "" {
				snap.Users[i].Name = name
			}
		}
	}
	if len(channelIDSet) > 0 {
		names := lookupRoutingChannelNames(queryCtx, sortedMapKeys(channelIDSet))
		for i := range snap.Channels {
			if name := names[snap.Channels[i].ID]; name != "" {
				snap.Channels[i].Name = name
			}
		}
		for i := range snap.Breakers {
			if name := names[snap.Breakers[i].ChannelID]; name != "" {
				snap.Breakers[i].ChannelName = name
			}
		}
		for i := range snap.Cooldowns {
			if name := names[snap.Cooldowns[i].ChannelID]; name != "" {
				snap.Cooldowns[i].ChannelName = name
			}
		}
	}
}

func sortedMapKeys(set map[int]struct{}) []int {
	ids := make([]int, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func lookupRoutingUserNames(ctx context.Context, ids []int) map[int]string {
	type userNameRow struct {
		Id          int
		Username    string
		DisplayName string
	}
	names := make(map[int]string, len(ids))
	for start := 0; start < len(ids); start += routingNameLookupBatchSize {
		end := min(start+routingNameLookupBatchSize, len(ids))
		var rows []userNameRow
		err := model.DB.WithContext(ctx).Unscoped().Model(&model.User{}).
			Select("id, username, display_name").
			Where("id IN ?", ids[start:end]).
			Find(&rows).Error
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				common.SysError(common.LogText("failed to resolve routing snapshot user names: %v", err))
			}
			return names
		}
		for _, row := range rows {
			name := strings.TrimSpace(row.Username)
			if name == "" {
				name = strings.TrimSpace(row.DisplayName)
			}
			if name != "" {
				names[row.Id] = name
			}
		}
	}
	return names
}

func lookupRoutingChannelNames(ctx context.Context, ids []int) map[int]string {
	type channelNameRow struct {
		Id   int
		Name string
	}
	names := make(map[int]string, len(ids))
	for start := 0; start < len(ids); start += routingNameLookupBatchSize {
		end := min(start+routingNameLookupBatchSize, len(ids))
		var rows []channelNameRow
		err := model.DB.WithContext(ctx).Model(&model.Channel{}).
			Select("id, name").
			Where("id IN ?", ids[start:end]).
			Find(&rows).Error
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				common.SysError(common.LogText("failed to resolve routing snapshot channel names: %v", err))
			}
			return names
		}
		for _, row := range rows {
			if name := strings.TrimSpace(row.Name); name != "" {
				names[row.Id] = name
			}
		}
	}
	return names
}

type RoutingRuntimeSnapshot struct {
	Breakers     []RouteBreakerSnapshot     `json:"breakers"`
	Cooldowns    []RouteCooldownSnapshot    `json:"cooldowns"`
	Quality      []RouteQualitySnapshot     `json:"quality"`
	Cache        RouteCacheDiagnostics      `json:"cache"`
	Users        []ConcurrencySnapshot      `json:"users"`
	Channels     []ConcurrencySnapshot      `json:"channels"`
	Observations []RouteObservationSnapshot `json:"observations"`
}

func RoutingSnapshot() RoutingRuntimeSnapshot {
	breakers, cooldowns := routeObservations.healthSnapshot()
	return RoutingRuntimeSnapshot{Users: gatewayConcurrency.snapshot("user"), Channels: gatewayConcurrency.snapshot("channel"), Observations: routeObservations.snapshot(), Breakers: breakers, Cooldowns: cooldowns, Quality: qualityDiagnostics(), Cache: routeCaches.diagnostics()}
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
	publication := gatewayConcurrency.publication
	gatewayConcurrency.pending[key] = publication
	if key.kind == "channel" {
		routeObservations.markChannelPublishPending(key.id, publication)
	}
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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %w", errConcurrencyResourceDeleted, err)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", errConcurrencyLimitUnavailable, err)
	}
	if limit < 0 {
		return fmt.Errorf("%w: invalid concurrency limit %d", errConcurrencyLimitUnavailable, limit)
	}
	gatewayConcurrency.setLimit(key.kind, key.id, limit)
	if key.kind == "channel" {
		routeObservations.invalidate(key.id)
	}
	gatewayConcurrency.mu.Lock()
	if gatewayConcurrency.pending[key] == publication {
		delete(gatewayConcurrency.pending, key)
		if key.kind == "channel" {
			routeObservations.clearChannelPublishPending(key.id, publication)
		}
	}
	gatewayConcurrency.mu.Unlock()
	return nil
}
func init() {
	model.ChannelRoutingChanged = func(id int) error { return RefreshChannelConcurrencyLimit(context.Background(), id) }
	model.ChannelRoutingMutating = func(ids []int) func() { return routeObservations.beginChannelMutation(ids) }
	model.ChannelRoutingDeleted = func(ids []int) {
		for _, id := range ids {
			routeObservations.invalidate(id)
			routeObservations.clearDeletedChannelPending(id)
		}
	}
}
