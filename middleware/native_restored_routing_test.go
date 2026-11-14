//go:build known_upstream_restored

package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredProbeGeminiUnknownAction(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:unknown", strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
	_, _, err := getModelRequest(c)
	assert.Error(t, err)
}
func TestRestoredProbeSetupErrorStopsHandler(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	oldDB, oldCache := model.DB, common.MemoryCacheEnabled
	model.DB = db
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { model.DB = oldDB; common.MemoryCacheEnabled = oldCache })
	ch := &model.Channel{Id: 983, Type: constant.ChannelTypeOpenAI, Name: "test", Models: "gpt-test", Key: "disabled-key", Status: common.ChannelStatusEnabled, ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 1, MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled}}}
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 983, Model: "gpt-test", Enabled: true}).Error)
	r := gin.New()
	called := false
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("id", 1)
		service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 983, Source: dto.PinSourceToken})
	}, Distribute(), func(c *gin.Context) { called = true; c.Status(204) })
	q := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	assert.False(t, called)
	assert.GreaterOrEqual(t, w.Code, 400)
	assert.Contains(t, w.Body.String(), "no enabled keys", "probe must reach key setup, not route admission")
}
