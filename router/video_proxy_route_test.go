package router

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPrivateVideoRouteRequiresOwnerAndTrustedOrigin(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}))
	cache, fetch := common.MemoryCacheEnabled, *system_setting.GetFetchSetting()
	common.MemoryCacheEnabled = false
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{EnableSSRFProtection: true, ApplyIPFilterForDomain: true, AllowedPorts: []string{"443"}}
	service.InitHttpClient()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = cache
		*system_setting.GetFetchSetting() = fetch
		service.InitHttpClient()
	})
	for id, name := range map[int]string{91: "owner", 92: "other"} {
		require.NoError(t, model.DB.Create(&model.User{Id: id, Username: name, AffCode: name, Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Quota: 100}).Error)
		require.NoError(t, model.DB.Create(&model.Token{UserId: id, Key: name, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}).Error)
	}
	var calls, foreignCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("video"))
	}))
	defer upstream.Close()
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignCalls.Add(1) }))
	defer foreign.Close()
	require.NoError(t, model.DB.Create(&model.Channel{Id: 17, Name: "media", Type: constant.ChannelTypeGemini, Key: "fixture", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled}).Error)
	task := &model.Task{TaskID: "task_private_video", Platform: "google", UserId: 91, ChannelId: 17, Status: model.TaskStatusSuccess, Progress: "100%", PrivateData: model.TaskPrivateData{Execution: &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{Key: "google", Name: "Google Veo (Gemini API)", Version: "1.2.0", APIVersion: 1}}}}
	setMedia := func(url string) {
		task.PrivateData.ResultURL = url
		task.SetData(map[string]any{"response": map[string]any{"generateVideoResponse": map[string]any{"generatedSamples": []any{map[string]any{"video": map[string]any{"uri": url}}}}}})
		require.NoError(t, model.DB.Save(task).Error)
	}
	setMedia(upstream.URL + "/video")
	engine := gin.New()
	SetTaskPluginProtocolRouter(engine)
	fetchMedia := func(user string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
		r.Header.Set("Authorization", "Bearer sk-"+user)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	owned := fetchMedia("owner")
	require.Equal(t, http.StatusOK, owned.Code, owned.Body.String())
	require.Equal(t, "video", owned.Body.String())
	require.EqualValues(t, 1, calls.Load())

	denied := fetchMedia("other")
	require.Equal(t, http.StatusNotFound, denied.Code, denied.Body.String())
	require.EqualValues(t, 1, calls.Load())

	setMedia(foreign.URL + "/video")
	rejected := fetchMedia("owner")
	require.Equal(t, http.StatusBadGateway, rejected.Code, rejected.Body.String())
	require.Zero(t, foreignCalls.Load())

	task.PrivateData.Execution = nil
	setMedia(upstream.URL + "/video")
	untrusted := fetchMedia("owner")
	require.Equal(t, http.StatusBadGateway, untrusted.Code, untrusted.Body.String())
	require.EqualValues(t, 1, calls.Load())
}
