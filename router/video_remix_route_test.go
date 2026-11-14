package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const remixUserID = 61

// remixUpstream records what the native remix route sends to the vendor.
type remixUpstream struct {
	mu       sync.Mutex
	failures int
	sends    int
	path     string
	body     string
	header   http.Header
}

func (u *remixUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	u.sends++
	u.path, u.body, u.header = r.URL.Path, string(body), r.Header.Clone()
	w.Header().Set("Content-Type", "application/json")
	if u.failures > 0 {
		u.failures--
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"message":"vendor unavailable"}}`)
		return
	}
	_, _ = io.WriteString(w, `{"id":"video_remix_up","status":"queued"}`)
}

type remixRoute struct {
	engine     *gin.Engine
	upstream   *remixUpstream
	baseURL    string
	panicValue any
	panicStack []byte
}

func newRemixRoute(t *testing.T) *remixRoute {
	t.Helper()
	require.NoError(t, appI18n.Init())
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.Log{}, &model.TopUpDiscountUsageHourly{}, &model.TopUpDiscountConfigVersion{}))
	service.InitHttpClient()
	previousPrices := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-2":0.1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices)) })

	upstream := &remixUpstream{}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	require.NoError(t, model.DB.Create(&model.User{Id: remixUserID, Username: "remix-user", Status: common.UserStatusEnabled, Quota: 1_000_000, AffCode: "remix-user"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: remixUserID + 1, Username: "remix-other", Status: common.UserStatusEnabled, Quota: 1_000_000, AffCode: "remix-other"}).Error)
	route := &remixRoute{upstream: upstream, baseURL: server.URL}
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		defer func() {
			if value := recover(); value != nil {
				route.panicValue, route.panicStack = value, debug.Stack()
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	})
	SetVideoRouter(engine)
	route.engine = engine
	return route
}

func (r *remixRoute) token(t *testing.T, key string, modelLimits string) string {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Token{
		UserId: remixUserID, Key: key, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true,
		ModelLimitsEnabled: modelLimits != "", ModelLimits: modelLimits,
	}).Error)
	return "Bearer sk-" + key
}

func (r *remixRoute) channel(t *testing.T, channel model.Channel) model.Channel {
	t.Helper()
	channel.BaseURL = &r.baseURL
	channel.Status = common.ChannelStatusEnabled
	if channel.Key == "" {
		channel.Key = "sk-remix"
	}
	require.NoError(t, model.DB.Create(&channel).Error)
	return channel
}

func (r *remixRoute) task(t *testing.T, task model.Task) {
	t.Helper()
	if task.UserId == 0 {
		task.UserId = remixUserID
	}
	task.Status = model.TaskStatusSuccess
	require.NoError(t, model.DB.Create(&task).Error)
}

func (r *remixRoute) remix(t *testing.T, authorization, videoID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/videos/"+videoID+"/remix", strings.NewReader(`{"prompt":"make it night"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", authorization)
	recorder := httptest.NewRecorder()
	r.panicValue, r.panicStack = nil, nil
	r.engine.ServeHTTP(recorder, request)
	return recorder
}

func remixUserQuota(t *testing.T) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, remixUserID).Error)
	return user.Quota
}

func remixTaskCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&count).Error)
	return count
}

func soraOriginTask(channelID int) model.Task {
	return model.Task{
		TaskID:     "task_origin_public",
		Platform:   constant.TaskPlatform("sora"),
		ChannelId:  channelID,
		Properties: model.Properties{OriginModelName: "sora-2"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "video_origin_up",
			BillingContext: &model.TaskBillingContext{OtherRatios: map[string]float64{"seconds": 12}},
		},
	}
}

// Unrestricted keys retain the upstream ownership check. Restricted-key
// continuations remain an upstream waiting item, covered by upstream_probe.
func TestNativeRemixRouteMissingOrForeignTaskIsNotFound(t *testing.T) {
	route := newRemixRoute(t)
	channel := route.channel(t, model.Channel{Type: constant.ChannelTypeSora, Name: "sora", Models: "sora-2"})
	foreign := soraOriginTask(channel.Id)
	foreign.UserId = remixUserID + 1
	route.task(t, foreign)
	authorization := route.token(t, "remixmissing", "")
	tasks := remixTaskCount(t)
	before := remixUserQuota(t)

	for _, videoID := range []string{"task_origin_public", "task_does_not_exist"} {
		recorder := route.remix(t, authorization, videoID)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		assert.Contains(t, recorder.Body.String(), "task_not_exist")
	}
	assert.Zero(t, route.upstream.sends)
	assert.Equal(t, before, remixUserQuota(t))
	assert.Equal(t, tasks, remixTaskCount(t))
}
