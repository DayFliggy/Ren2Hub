package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskReadsHonorTokenModelLimitsAcrossProtocols(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body string
	}{
		{"video", http.MethodGet, "/v1/video/generations/task-scope", ""},
		{"suno-single", http.MethodGet, "/suno/fetch/task-scope", ""},
		{"suno-batch", http.MethodPost, "/suno/fetch", `{"ids":["task-scope"]}`},
		{"mj-single", http.MethodGet, "/mj/task/task-scope/fetch", ""},
		{"mj-batch", http.MethodPost, "/mj/task/list-by-condition", `{"ids":["task-scope"]}`},
		{"mj-seed", http.MethodGet, "/mj/task/task-scope/image-seed", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupRelayRouterTestDB(t)
			db := model.DB
			require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.Midjourney{}, &model.Log{}))
			require.NoError(t, controller.InitTaskPlugins())
			user := &model.User{Username: "task-scope", Password: "test-password", Status: common.UserStatusEnabled, Quota: 100_000}
			require.NoError(t, db.Create(user).Error)
			token := &model.Token{UserId: user.Id, Key: "taskscopetestkey", Name: "scope", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true, ModelLimitsEnabled: true, ModelLimits: "gpt-4o-mini"}
			require.NoError(t, db.Create(token).Error)
			channel := &model.Channel{Type: constant.ChannelTypeMidjourney, Status: common.ChannelStatusEnabled, Key: "test-key", Name: "scope", Models: "mj_imagine", Group: "default"}
			require.NoError(t, db.Create(channel).Error)
			sunoapi, found := jsplugin.DefaultRegistry.Get("sunoapi")
			require.True(t, found)
			version, err := model.GetTaskPluginVersion("sunoapi", sunoapi.Meta.Version)
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Task{UserId: user.Id, TaskID: "task-scope", Platform: constant.TaskPlatform("sunoapi"), Action: "MUSIC", Status: model.TaskStatusSuccess, Properties: model.Properties{OriginModelName: "suno_music"}, PrivateData: model.TaskPrivateData{TokenId: token.Id, PluginRef: &model.TaskPluginRef{Key: version.Key, Version: version.Version, SourceHash: version.SourceHash}}}).Error)
			require.NoError(t, db.Create(&model.Midjourney{UserId: user.Id, MjId: "task-scope", Action: constant.MjActionImagine, Prompt: "protected", ChannelId: channel.Id, Status: "SUCCESS", PrivateData: model.TaskPrivateData{TokenId: token.Id}}).Error)

			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			SetVideoRouter(engine)
			engine.NoRoute(SetPluginRouter(engine))
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token.Key)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			wantStatus := http.StatusForbidden
			if strings.HasPrefix(test.name, "suno-") {
				wantStatus = http.StatusNotFound
			}
			assert.Equal(t, wantStatus, response.Code, response.Body.String())
			assert.NotContains(t, response.Body.String(), "protected")
		})
	}
}
