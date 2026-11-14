package router

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type videoRatePipeline struct {
	engine        *gin.Engine
	userID        int
	tokenID       int
	authorization string
	claimed       bool
	names         []string
}

// Register both production entry points. The observer records the actual Gin
// chain; it neither injects identity nor replaces authentication or a handler.
func registerVideoRatePipeline() *videoRatePipeline {
	p := &videoRatePipeline{engine: gin.New()}
	p.engine.Use(func(c *gin.Context) {
		p.names = append([]string(nil), c.HandlerNames()...)
		c.Next()
		_, p.claimed = c.Get(jsplugin.ContextKeyPinnedEndpoint)
	})
	SetVideoRouter(p.engine)
	SetTaskPluginProtocolRouter(p.engine)
	return p
}

func newVideoRatePipeline(t *testing.T) *videoRatePipeline {
	t.Helper()
	setupRelayRouterTestDB(t)
	require.NoError(t, appI18n.Init())
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Model{}, &model.Vendor{}, &model.Option{}, &model.Task{}))
	previousEnabled, previousCount, previousSuccess, previousWindow := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes
	previousMemory := common.MemoryCacheEnabled
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = true, 1, 0, 1
	common.MemoryCacheEnabled = false
	registry := jsplugin.NewRegistry()
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	_, err = registry.RegisterFactory(source, jsplugin.Options{Key: "sora"})
	require.NoError(t, err)
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = registry
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousCount, previousSuccess, previousWindow
		common.MemoryCacheEnabled = previousMemory
		jsplugin.DefaultRegistry = previousRegistry
	})
	p := registerVideoRatePipeline()
	hash := fnv.New32a()
	_, err = hash.Write([]byte(t.Name()))
	require.NoError(t, err)
	p.userID = 91000000 + int(hash.Sum32()%10000000)
	user := model.User{Id: p.userID, Username: fmt.Sprintf("video-rate-%d", p.userID), Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Quota: 1000000}
	require.NoError(t, model.DB.Create(&user).Error)
	token := model.Token{UserId: p.userID, Key: fmt.Sprintf("videoratetoken%d", p.userID), Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000}
	require.NoError(t, model.DB.Create(&token).Error)
	p.tokenID = token.Id
	p.authorization = "Bearer sk-" + token.Key
	return p
}

func (p *videoRatePipeline) request(path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", p.authorization)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	p.claimed = false
	p.engine.ServeHTTP(recorder, request)
	return recorder
}
func (p *videoRatePipeline) assertNoChargeOrTask(t *testing.T) {
	t.Helper()
	var user model.User
	var token model.Token
	var tasks int64
	require.NoError(t, model.DB.First(&user, p.userID).Error)
	require.NoError(t, model.DB.First(&token, p.tokenID).Error)
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&tasks).Error)
	assert.Equal(t, 1000000, user.Quota, "wallet must remain unchanged")
	assert.Equal(t, 1000000, token.RemainQuota, "token quota must remain unchanged")
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, token.UsedQuota)
	assert.Zero(t, tasks)
}

const unclaimedVideoRateBody = `{"model":"video-rate-unclaimed","prompt":"pipeline probe","seconds":"4"}`
const claimedInvalidVideoRateBody = `{"model":"sora-2","prompt":"pipeline probe","seconds":0}`
