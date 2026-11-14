//go:build known_upstream_restored

package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func round3ServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	old, oldMemory, oldRedis, oldPath, oldMaster := model.DB, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath, common.IsMasterNode
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SQLitePath = t.TempDir() + "/route.db"
	common.IsMasterNode = false
	common.MemoryCacheEnabled = false
	common.RedisEnabled = false
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Token{}, &model.SystemTask{}))
	round3ExtraSchema(t, db)
	t.Cleanup(func() {
		model.DB = old
		common.MemoryCacheEnabled = oldMemory
		common.RedisEnabled = oldRedis
		common.SQLitePath = oldPath
		common.IsMasterNode = oldMaster
		common.SetDatabaseTypes(oldMain, oldLog)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.Token{Id: 7, UserId: 17, Key: "round3fixture"}).Error)
	return db
}
func round3Context(t *testing.T) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("token_id", 7)
	c.Set("id", 17)
	c.Set(common.RequestIdKey, "round3fixture")
	round3Release(t, c)
	return c
}
func round3Channel(t *testing.T, db *gorm.DB, id int) {
	ch := model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Key: "fixture", Name: "route", Models: "route-model", Status: common.ChannelStatusEnabled, Weight: common.GetPointer(uint(100))}
	round3Group(&ch)
	require.NoError(t, db.Create(&ch).Error)
	require.NoError(t, ch.AddAbilities(db))
}
func TestRestoredRound3CompactSelectionSkipsUnsupported(t *testing.T) {
	db := round3ServiceDB(t)
	round3Channel(t, db, 11)
	round3Channel(t, db, 22)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Updates(map[string]any{"type": constant.ChannelTypeAnthropic, "priority": 10}).Error)
	selectFor := func(path string) (*model.Channel, *ChannelSelectError) {
		c := round3Context(t)
		GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: path})
		return round3Select(c, "route-model")
	}
	selected, selectErr := selectFor("/v1/responses/compact")
	require.Nil(t, selectErr)
	assert.Equal(t, 22, selected.Id, "the higher-priority Anthropic channel cannot compact")
	selected, selectErr = selectFor("/v1/responses")
	require.Nil(t, selectErr)
	require.Equal(t, 11, selected.Id, "only the compact endpoint is narrowed")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("type", constant.ChannelTypeAnthropic).Error)
	_, selectErr = selectFor("/v1/responses/compact")
	if assert.NotNil(t, selectErr) {
		assert.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
		assert.True(t, selectErr.NoAvailableChannel)
		assert.Equal(t, i18n.MsgDistributorNoAvailableChannel, selectErr.MessageID)
		assert.Equal(t, map[string]any{"Model": "route-model"}, selectErr.Params)
	}
}
func TestRestoredRound3StrictAffinityErrorShape(t *testing.T) {
	db := round3ServiceDB(t)
	round3Channel(t, db, 22)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("status", common.ChannelStatusManuallyDisabled).Error)
	setting := operation_setting.GetChannelAffinitySetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	rule := operation_setting.ChannelAffinityRule{Name: "round3-affinity", ModelRegex: []string{"^route-model$"}, SessionMode: "strict", KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}}}
	encoded, err := common.Marshal([]operation_setting.ChannelAffinityRule{rule})
	require.NoError(t, err)
	snapshot, err := model.BuildRequestPolicy(map[string]string{"channel_affinity_setting.enabled": "true", "channel_affinity_setting.session_mode": "strict", "channel_affinity_setting.rules": string(encoded)})
	require.NoError(t, err)
	*setting = snapshot.Affinity
	key := round3AffinityKey(rule, "route-model", t.Name())
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(key, 22, time.Minute))
	t.Cleanup(func() { _, _ = cache.DeleteMany([]string{key}) })
	c := round3Context(t)
	c.Request.Header.Set("X-Session", t.Name())
	selected, selectErr := round3Select(c, "route-model")
	require.Nil(t, selected)
	require.NotNil(t, selectErr)
	require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
	assert.EqualValues(t, "session_channel_unavailable", selectErr.Code)
	assert.Equal(t, "Bound session channel is unavailable", selectErr.Message)
}
