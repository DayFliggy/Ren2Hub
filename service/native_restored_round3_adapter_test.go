//go:build known_upstream_restored

package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func round3ExtraSchema(t *testing.T, db *gorm.DB) {
	require.NoError(t, db.AutoMigrate(&model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	previous := gatewayConcurrency
	gatewayConcurrency = NewConcurrencyManager()
	t.Cleanup(func() { gatewayConcurrency = previous })
}
func round3Group(ch *model.Channel) {}
func round3Select(c *gin.Context, name string) (*model.Channel, *ChannelSelectError) {
	return SelectChannelForRequest(c, name, nil)
}
func round3Release(t *testing.T, c *gin.Context) { t.Cleanup(func() { ReleaseRouteChannel(c) }) }
func round3AffinityKey(rule operation_setting.ChannelAffinityRule, name, value string) string {
	return buildChannelAffinityCacheKeySuffix(rule, name, value)
}
