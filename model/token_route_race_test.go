package model

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 并发屏障：被暂停的一方停在指定阶段，另一方运行到结束或在数据库锁上等待之后才放行，
// 两边的先后由数据库状态决定，不依赖睡眠时长。
const (
	stageEntriesRead   = "entries_read"   // 保存：读完旧条目、首次写入之前
	stageEntriesInsert = "entries_insert" // 保存：渠道已核验、条目未插入
	stageRoutesDeleted = "routes_deleted" // 删除：已删路由、未删 Token
)

type routeBarrier struct {
	mu      sync.Mutex
	stage   string
	reached chan struct{}
	resume  chan struct{}
}

func installRouteBarrier(t *testing.T, db *gorm.DB) *routeBarrier {
	t.Helper()
	barrier := &routeBarrier{}
	hook := func(stage string, target any) func(*gorm.DB) {
		want := reflect.TypeOf(target)
		return func(tx *gorm.DB) {
			if tx.Error == nil && tx.Statement.Schema != nil && tx.Statement.Schema.ModelType == want {
				barrier.pause(stage)
			}
		}
	}
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:route_entries_read", hook(stageEntriesRead, TokenRouteEntry{})))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:route_entries_insert", hook(stageEntriesInsert, TokenRouteEntry{})))
	require.NoError(t, db.Callback().Delete().After("gorm:delete").Register("test:route_profile_deleted", hook(stageRoutesDeleted, TokenRouteProfile{})))
	return barrier
}

func (b *routeBarrier) arm(stage string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stage, b.reached, b.resume = stage, make(chan struct{}), make(chan struct{})
}

func (b *routeBarrier) pause(stage string) {
	b.mu.Lock()
	if b.stage != stage {
		b.mu.Unlock()
		return
	}
	b.stage = ""
	reached, resume := b.reached, b.resume
	b.mu.Unlock()
	close(reached)
	<-resume
}

// race 让 paused 停在 stage，再运行 concurrent。concurrentFirst 表示 concurrent 在 paused
// 放行之前已经结束；为 false 时 concurrent 在等 paused 持有的数据库锁。
func (b *routeBarrier) race(t *testing.T, db *gorm.DB, stage string, paused, concurrent func() error) (concurrentFirst bool, pausedErr, concurrentErr error) {
	t.Helper()
	b.arm(stage)
	release := sync.OnceFunc(func() { close(b.resume) })
	defer release()
	pausedDone, concurrentDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(pausedDone)
		pausedErr = paused()
	}()
	select {
	case <-b.reached:
	case <-pausedDone:
		t.Fatalf("paused operation finished without reaching %s: %v", stage, pausedErr)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	baseline := sqlDB.Stats().WaitCount
	go func() {
		defer close(concurrentDone)
		concurrentErr = concurrent()
	}()
	concurrentFirst = waitFinishedOrBlocked(t, db, sqlDB, baseline, concurrentDone)
	release()
	<-pausedDone
	<-concurrentDone
	return concurrentFirst, pausedErr, concurrentErr
}

// waitFinishedOrBlocked 轮询状态而不是等待固定时长：concurrent 结束返回 true，进入锁等待返回 false。
func waitFinishedOrBlocked(t *testing.T, db *gorm.DB, sqlDB *sql.DB, baseline int64, done <-chan struct{}) bool {
	t.Helper()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return true
		case <-ticker.C:
			if lockWaiters(t, db, sqlDB, baseline) > 0 {
				return false
			}
		}
	}
}

// lockWaiters 返回正在等数据库锁的事务数。屏障停住的一方在 Go 回调里，不算等待者。
// 测试库必须独占：查询按实例或库统计，不区分夹具。SQLite 测试库只有一个连接，等待表现为连接池排队。
func lockWaiters(t *testing.T, db *gorm.DB, sqlDB *sql.DB, baseline int64) int64 {
	t.Helper()
	var waiting int64
	switch common.MainDatabaseType() {
	case common.DatabaseTypeSQLite:
		return sqlDB.Stats().WaitCount - baseline
	case common.DatabaseTypeMySQL:
		// MySQL 手册：innodb_trx 读的是中间缓存，距上次读取不到 0.1 秒就不刷新；2ms 轮询时实测
		// 一直停在第一次读到的状态。performance_schema.data_lock_waits 直接读锁表。
		require.NoError(t, db.Raw("SELECT COUNT(DISTINCT REQUESTING_ENGINE_TRANSACTION_ID) FROM performance_schema.data_lock_waits").Scan(&waiting).Error)
	case common.DatabaseTypePostgreSQL:
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").Scan(&waiting).Error)
	default:
		t.Fatalf("unsupported database type %s", common.MainDatabaseType())
	}
	return waiting
}

type raceFixture struct {
	db    *gorm.DB
	token Token
	ids   map[string]int
}

// seedRaceToken 建一个 Token 和渠道 A、B；labels 非 nil 时先保存为这些条目的手动路由（版本 2）。
func seedRaceToken(t *testing.T, db *gorm.DB, index int, labels []string) raceFixture {
	t.Helper()
	fixture := raceFixture{db: db, token: Token{UserId: 900 + index, Key: fmt.Sprintf("routing-race-%d", index)}, ids: map[string]int{}}
	for offset, label := range []string{"A", "B"} {
		id := 930000 + index*10 + offset
		require.NoError(t, db.Create(&Channel{Id: id, Key: fmt.Sprintf("race-%d", id), Status: common.ChannelStatusEnabled, Models: "route-model"}).Error)
		fixture.ids[label] = id
	}
	if labels == nil {
		require.NoError(t, fixture.token.Insert())
		return fixture
	}
	require.NoError(t, fixture.token.InsertWithRouting(TokenRouteManual))
	_, err := SaveTokenRouting(context.Background(), fixture.token.UserId, fixture.token.Id, fixture.route(1, labels...), fixture.allowed())
	require.NoError(t, err)
	return fixture
}

func (f raceFixture) route(version int64, labels ...string) TokenRouting {
	route := TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: version}}
	for _, label := range labels {
		route.Entries = append(route.Entries, TokenRouteEntry{ChannelID: f.ids[label], Enabled: true})
	}
	return route
}

// allowed 是保存前在事务外读到的结果，渠道随后可能被删除。
func (f raceFixture) allowed() map[int]bool {
	return map[int]bool{f.ids["A"]: true, f.ids["B"]: true}
}

func (f raceFixture) save(input TokenRouting) error {
	_, err := SaveTokenRouting(context.Background(), f.token.UserId, f.token.Id, input, f.allowed())
	return err
}

// deleteChannelA 与管理端删除渠道相同：删渠道和能力，再清理指向已不存在渠道的条目。
func (f raceFixture) deleteChannelA() error {
	channel := Channel{Id: f.ids["A"]}
	if err := channel.Delete(); err != nil {
		return err
	}
	return DeleteOrphanChannelRouteEntries(context.Background())
}

func (f raceFixture) deleteToken(batch bool) error {
	if batch {
		count, err := BatchDeleteTokens([]int{f.token.Id}, f.token.UserId)
		if err == nil && count != 1 {
			return fmt.Errorf("batch delete removed %d tokens", count)
		}
		return err
	}
	token, err := GetTokenByIds(f.token.Id, f.token.UserId)
	if err != nil {
		return err
	}
	return token.Delete()
}

// hardDeleteOwner 建出 Token 的主人，返回与管理端相同的硬删除用户操作。
func (f raceFixture) hardDeleteOwner(t *testing.T) func() error {
	t.Helper()
	owner := User{Id: f.token.UserId, Username: fmt.Sprintf("race-owner-%d", f.token.UserId), AffCode: fmt.Sprintf("race-aff-%d", f.token.UserId), Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&owner).Error)
	return func() error {
		_, err := owner.HardDelete()
		return err
	}
}

func (f raceFixture) entryLabels(t *testing.T) []string {
	t.Helper()
	var entries []TokenRouteEntry
	require.NoError(t, f.db.Where("token_id = ?", f.token.Id).Order("position asc").Find(&entries).Error)
	labels := []string{}
	for _, entry := range entries {
		for label, id := range f.ids {
			if id == entry.ChannelID {
				labels = append(labels, label)
			}
		}
	}
	return labels
}

func (f raceFixture) requireTokenGoneWithoutRouting(t *testing.T) {
	t.Helper()
	var tokens, profiles, entries int64
	require.NoError(t, f.db.Model(&Token{}).Where("id = ?", f.token.Id).Count(&tokens).Error)
	require.NoError(t, f.db.Model(&TokenRouteProfile{}).Where("token_id = ?", f.token.Id).Count(&profiles).Error)
	require.NoError(t, f.db.Model(&TokenRouteEntry{}).Where("token_id = ?", f.token.Id).Count(&entries).Error)
	require.Zero(t, tokens, "token is soft-deleted")
	require.Zero(t, profiles, "no orphan profile")
	require.Zero(t, entries, "no orphan entries")
}

// requireNoOrphanRoutes 检查全库：条目指向的渠道和 Token 都存在，profile 的 Token 也存在。
func requireNoOrphanRoutes(t *testing.T, db *gorm.DB) {
	t.Helper()
	var channelIDs, tokenIDs []int
	var entries []TokenRouteEntry
	var profiles []TokenRouteProfile
	require.NoError(t, db.Model(&Channel{}).Pluck("id", &channelIDs).Error)
	require.NoError(t, db.Model(&Token{}).Pluck("id", &tokenIDs).Error)
	require.NoError(t, db.Find(&entries).Error)
	require.NoError(t, db.Find(&profiles).Error)
	for _, entry := range entries {
		require.Truef(t, slices.Contains(channelIDs, entry.ChannelID), "entry %+v points to a deleted channel", entry)
		require.Truef(t, slices.Contains(tokenIDs, entry.TokenID), "entry %+v belongs to a deleted token", entry)
	}
	for _, profile := range profiles {
		require.Truef(t, slices.Contains(tokenIDs, profile.TokenID), "profile %+v belongs to a deleted token", profile)
	}
}

// runTokenRouteRaceCases 是 B5、B11 的屏障用例，由 TestTokenRoutingDatabases 在每种数据库上调用。
// 只断言三库共同的不变量：最终没有孤儿条目和孤儿 profile；锁住的行被删除时一方等另一方，
// 不会两边都成功写坏数据。concurrentFirst 记录实际先后，SQLite 的写事务串行，结果与锁序无关。
func runTokenRouteRaceCases(t *testing.T, db *gorm.DB, barrier *routeBarrier) {
	saveCases := []struct {
		name, stage     string
		initial, input  []string
		op              string // channel_delete、token_delete、batch_token_delete
		expect          []string
		forbiddenIfLost bool // 删除先完成时，输入里仍有已删除渠道，保存返回 403
	}{
		{name: "entries_read-keep_A-channel_delete", stage: stageEntriesRead, initial: []string{"A", "B"}, input: []string{"A", "B"}, op: "channel_delete", expect: []string{"B"}, forbiddenIfLost: true},
		{name: "entries_read-remove_A-channel_delete", stage: stageEntriesRead, initial: []string{"A", "B"}, input: []string{"B"}, op: "channel_delete", expect: []string{"B"}},
		{name: "entries_read-add_A-channel_delete", stage: stageEntriesRead, initial: []string{"B"}, input: []string{"B", "A"}, op: "channel_delete", expect: []string{"B"}, forbiddenIfLost: true},
		{name: "entries_read-keep_A-token_delete", stage: stageEntriesRead, initial: []string{"A", "B"}, input: []string{"A", "B"}, op: "token_delete"},
		{name: "entries_insert-keep_A-channel_delete", stage: stageEntriesInsert, initial: []string{"A", "B"}, input: []string{"A", "B"}, op: "channel_delete", expect: []string{"B"}, forbiddenIfLost: true},
		{name: "entries_insert-add_A-channel_delete", stage: stageEntriesInsert, initial: []string{"B"}, input: []string{"B", "A"}, op: "channel_delete", expect: []string{"B"}, forbiddenIfLost: true},
		{name: "entries_insert-keep_A-batch_token_delete", stage: stageEntriesInsert, initial: []string{"A", "B"}, input: []string{"A", "B"}, op: "batch_token_delete"},
	}
	for index, tc := range saveCases {
		t.Run("save-"+tc.name, func(t *testing.T) {
			fixture := seedRaceToken(t, db, index, tc.initial)
			concurrent := fixture.deleteChannelA
			if tc.op != "channel_delete" {
				concurrent = func() error { return fixture.deleteToken(tc.op == "batch_token_delete") }
			}
			first, saveErr, deleteErr := barrier.race(t, db, tc.stage, func() error { return fixture.save(fixture.route(2, tc.input...)) }, concurrent)
			t.Logf("dialect=%s concurrent_first=%t save_err=%v delete_err=%v", common.MainDatabaseType(), first, saveErr, deleteErr)
			require.NoError(t, deleteErr)
			if tc.stage == stageEntriesInsert || tc.op != "channel_delete" {
				require.False(t, first, "deleting a locked channel or token waits for the save")
			}
			if tc.forbiddenIfLost && first {
				require.ErrorIs(t, saveErr, ErrTokenRouteForbidden)
			} else {
				require.NoError(t, saveErr)
			}
			if tc.op == "channel_delete" {
				require.Equal(t, tc.expect, fixture.entryLabels(t))
			} else {
				fixture.requireTokenGoneWithoutRouting(t)
			}
			requireNoOrphanRoutes(t, db)
		})
	}
	deleteCases := []struct {
		name                   string
		batch, saved, hardUser bool
	}{
		{name: "single-first_save"},
		{name: "single-existing_profile", saved: true},
		{name: "batch-first_save", batch: true},
		{name: "batch-existing_profile", batch: true, saved: true},
		{name: "hard_user-first_save", hardUser: true},
		{name: "hard_user-existing_profile", hardUser: true, saved: true},
	}
	for index, tc := range deleteCases {
		t.Run("delete-"+tc.name, func(t *testing.T) {
			var labels []string
			version := int64(0)
			if tc.saved {
				labels, version = []string{"B"}, 2
			}
			fixture := seedRaceToken(t, db, len(saveCases)+index, labels)
			remove := func() error { return fixture.deleteToken(tc.batch) }
			if tc.hardUser {
				remove = fixture.hardDeleteOwner(t)
			}
			first, deleteErr, saveErr := barrier.race(t, db, stageRoutesDeleted, remove, func() error { return fixture.save(fixture.route(version, "A", "B")) })
			t.Logf("dialect=%s concurrent_first=%t delete_err=%v save_err=%v", common.MainDatabaseType(), first, deleteErr, saveErr)
			require.NoError(t, deleteErr)
			require.False(t, first, "a save waits for the token deletion that started first")
			require.ErrorIs(t, saveErr, gorm.ErrRecordNotFound)
			fixture.requireTokenGoneWithoutRouting(t)
			requireNoOrphanRoutes(t, db)
		})
	}
	t.Run("delete-hard_user-batch_multi_token", func(t *testing.T) { runMultiTokenDeleteRace(t, db) })
}

// runMultiTokenDeleteRace：同一用户有 Token A、B，A.ID < B.ID，B 先写入，在表里排在 A 前面。
// 另一个事务先锁住 A，让 HardDelete 排在 A 上，再让批删同时删 A、B。批删若不按 ID 顺序加锁，
// 会先锁 B 再等 A；放行后 HardDelete 拿到 A 又等 B，形成等待环，数据库中止其中一方。
// 只有 PostgreSQL 能复现：MySQL 的无序锁定读走主键范围扫描，本来就先 A 后 B。
func runMultiTokenDeleteRace(t *testing.T, db *gorm.DB) {
	const userID, lowID, highID = 990, 940001, 940002
	hardDelete := raceFixture{db: db, token: Token{UserId: userID}}.hardDeleteOwner(t)
	for _, id := range []int{highID, lowID} {
		token := Token{Id: id, UserId: userID, Key: fmt.Sprintf("routing-multi-%d", id)}
		require.NoError(t, token.InsertWithRouting(TokenRouteManual))
	}
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		requireUnorderedLockReachesHighIDFirst(t, db, userID, lowID, highID)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	holder := db.Begin()
	require.NoError(t, holder.Error)
	defer holder.Rollback()
	var locked []int
	require.NoError(t, lockForUpdate(holder.Model(&Token{})).Where("id = ?", lowID).Pluck("id", &locked).Error)
	baseline := sqlDB.Stats().WaitCount
	hardDone, batchDone := make(chan error, 1), make(chan error, 1)
	go func() { hardDone <- hardDelete() }()
	waitLockWaiters(t, db, sqlDB, baseline, 1)
	go func() {
		_, err := BatchDeleteTokens([]int{lowID, highID}, userID)
		batchDone <- err
	}()
	waitLockWaiters(t, db, sqlDB, baseline, 2)
	require.NoError(t, holder.Commit().Error)
	require.NoError(t, <-hardDone, "hard delete")
	require.NoError(t, <-batchDone, "batch delete")
	var remaining int64
	require.NoError(t, db.Unscoped().Model(&Token{}).Where("user_id = ?", userID).Count(&remaining).Error)
	require.Zero(t, remaining, "hard delete removes both tokens")
	requireNoOrphanRoutes(t, db)
}

// requireUnorderedLockReachesHighIDFirst 按修复前批删的写法（锁定读、无 ORDER BY）执行一次再回滚，
// 确认它先锁到高 ID。表布局或执行计划变了、前提不成立时在这里失败，不让去掉排序的回归空过。
func requireUnorderedLockReachesHighIDFirst(t *testing.T, db *gorm.DB, userID, lowID, highID int) {
	t.Helper()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	var tokens []Token
	require.NoError(t, lockForUpdate(tx).Where("user_id = ? AND id IN (?)", userID, []int{lowID, highID}).Find(&tokens).Error)
	order := make([]int, 0, len(tokens))
	for _, token := range tokens {
		order = append(order, token.Id)
	}
	require.Equal(t, []int{highID, lowID}, order, "fixture must make an unordered locking read reach the higher ID first")
}

// waitLockWaiters 等到 want 个事务在数据库锁上排队；期限只用于失败时退出，不决定先后。
func waitLockWaiters(t *testing.T, db *gorm.DB, sqlDB *sql.DB, baseline, want int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for lockWaiters(t, db, sqlDB, baseline) < want {
		require.Truef(t, time.Now().Before(deadline), "expected %d transactions waiting on database locks", want)
		time.Sleep(2 * time.Millisecond)
	}
}
