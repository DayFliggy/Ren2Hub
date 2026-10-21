package service

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http/httptest"
	"testing"
)

func setupRouteTest(t *testing.T) *gorm.DB {
	t.Helper()
	previous, cache, redis, manager := model.DB, common.MemoryCacheEnabled, common.RedisEnabled, gatewayConcurrency
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	require.NoError(t, db.Create(&model.Token{Id: 7, UserId: 17, Key: "route-fixture"}).Error)
	model.DB = db
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	gatewayConcurrency = NewConcurrencyManager()
	t.Cleanup(func() {
		model.DB = previous
		common.MemoryCacheEnabled = cache
		common.RedisEnabled = redis
		gatewayConcurrency = manager
		require.NoError(t, sqlDB.Close())
	})
	return db
}
func createRouteChannel(t *testing.T, db *gorm.DB, id int, name string) {
	t.Helper()
	c := model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: fmt.Sprint("test-", id), Name: fmt.Sprint("channel-", id), Models: name, Status: common.ChannelStatusEnabled, Weight: common.GetPointer(uint(100))}
	require.NoError(t, db.Create(&c).Error)
	require.NoError(t, c.AddAbilities(nil))
}
func routeTestContext(t *testing.T) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("token_id", 7)
	c.Set("id", 17)
	c.Set(common.RequestIdKey, common.NewRequestId())
	t.Cleanup(func() { ReleaseRouteChannel(c) })
	return c
}

// resetRouteObservations isolates the process-wide observation store.
func resetRouteObservations(t *testing.T) {
	t.Helper()
	s := &routeObservations
	rows, breakers, cooldowns, changed, last := s.rows, s.breakers, s.cooldowns, s.changed, s.lastPrune
	s.rows = map[routeObservationKey]RouteObservationSnapshot{}
	s.breakers = map[routeBreakerKey]routeBreakerState{}
	s.cooldowns = map[int]int64{}
	s.changed = map[int]uint64{}
	t.Cleanup(func() {
		s.rows, s.breakers, s.cooldowns, s.changed, s.lastPrune = rows, breakers, cooldowns, changed, last
	})
}
func saveRouteTest(t *testing.T, entries ...int) {
	t.Helper()
	r := model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual}}
	allowed := map[int]bool{}
	for _, id := range entries {
		r.Entries = append(r.Entries, model.TokenRouteEntry{ChannelID: id, Enabled: true})
		allowed[id] = true
	}
	_, err := model.SaveTokenRouting(context.Background(), 17, 7, r, allowed)
	require.NoError(t, err)
}
