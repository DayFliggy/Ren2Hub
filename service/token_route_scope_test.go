package service

import (
	"maps"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// E2-3：同一请求的首轮和重试共用一份 Key 路由快照；长连接复核仍每次从库重读。
func TestRetryReusesRequestRouteSnapshot(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	saveRouteTest(t, 11, 22)
	queries := map[string]int{}
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("routing:count", func(tx *gorm.DB) {
		queries[tx.Statement.Table]++
	}))
	c := routeTestContext(t)
	first, selectErr := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 11, first.Id)
	require.Equal(t, 1, queries["token_route_profiles"])
	require.Equal(t, 1, queries["tokens"])
	AppendUsedChannel(c, first.Id)

	before := maps.Clone(queries)
	retry := 1
	second, selectErr := SelectChannelForRequest(c, "route-model", &RetryParam{Retry: &retry})
	require.Nil(t, selectErr)
	require.Equal(t, 22, second.Id)
	for _, table := range []string{"token_route_profiles", "token_route_entries", "tokens"} {
		require.Equal(t, before[table], queries[table], table)
	}
	require.Equal(t, before["abilities"]+1, queries["abilities"], "selection reads abilities once per attempt")

	require.NoError(t, db.Model(&model.TokenRouteEntry{}).Where("token_id = ? AND channel_id = ?", 7, 22).Update("enabled", false).Error)
	before = maps.Clone(queries)
	require.ErrorIs(t, CheckLockedTokenRoute(c, 22, "route-model"), model.ErrTokenRouteForbidden)
	require.Equal(t, before["token_route_profiles"]+1, queries["token_route_profiles"], "a long connection re-reads the route")
}
