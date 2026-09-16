package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayAdmissionSharesUserAcrossGenerationEntrypoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}))
	one, zero := 1, 0
	const userID = 88031
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "shared-generation-user", Status: common.UserStatusEnabled, GatewayConcurrency: &one, GatewayWaitMS: &zero}).Error)
	policy, err := service.LoadGatewayAdmissionPolicy(context.Background(), userID)
	require.NoError(t, err)
	holder, err := service.AcquireGatewayUserPermit(context.Background(), "first-token", userID, policy, service.NewGatewayWaitBudget(0))
	require.NoError(t, err)
	defer holder.Release()

	tests := []struct {
		name, method, path, body string
		admitted                 bool
	}{
		{"text", "POST", "/v1/chat/completions", `{"model":"gpt-test","messages":[]}`, true},
		{"stream", "POST", "/v1/messages", `{"model":"claude-test","stream":true,"messages":[]}`, true},
		{"remix", "POST", "/v1/videos/public-task/remix", `{"prompt":"remix"}`, true},
		{"suno-continuation", "POST", "/suno/submit/music", `{"task_id":"public-task","continue_clip_id":"clip"}`, true},
		{"midjourney", "POST", "/mj/submit/imagine", `{"prompt":"a cat"}`, true},
		{"realtime", "GET", "/v1/realtime?model=gpt-realtime", "", true},
		{"suno-query", "GET", "/suno/fetch/public-task", "", false},
		{"midjourney-query", "GET", "/mj/task/public-task/fetch", "", false},
		{"notification", "POST", "/mj/notify", "{}", false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("id", userID)
				c.Set(common.RequestIdKey, fmt.Sprintf("other-token-%d", index))
				common.SetContextKey(c, constant.ContextKeyTokenId, index+2)
				c.Params = append(c.Params, gin.Param{Key: "action", Value: "music"})
			}, GatewayAdmission())
			calls := 0
			router.Any("/*path", func(c *gin.Context) { calls++; c.Status(http.StatusOK) })
			response := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if test.admitted {
				assert.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
				assert.Zero(t, calls, "full user capacity must stop before any upstream handler")
			} else {
				assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
				assert.Equal(t, 1, calls)
			}
			assert.Equal(t, int64(1), service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(userID)).InFlight)
		})
	}
}

func TestGatewayAdmissionDoesNotWriteOrContinueAfterDisconnect(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	abortGatewayAdmission(c, service.ErrGatewayDraining)
	assert.True(t, c.IsAborted())
	assert.Empty(t, recorder.Body.String())
}
