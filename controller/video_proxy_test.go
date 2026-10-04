package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteVideoDataURLUsesPrivateCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	require.NoError(t, writeVideoDataURL(c, "data:video/mp4;base64,AAE="))
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, "private, max-age=86400", recorder.Header().Get("Cache-Control"))
}

func TestVideoProxyReturnsGoneForLegacyDataURI(t *testing.T) {
	db := setupVideoTaskLifecycle(t)
	task := &model.Task{
		TaskID:   "legacy-data-video",
		UserId:   1,
		Platform: constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeKling)),
		Action:   constant.TaskActionTextToVideo,
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
	}
	task.PrivateData.ResultURL = "data:video/mp4;base64,AAE="
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil).WithContext(context.Background())
	c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	c.Set("id", 1)
	VideoProxy(c)

	require.Equal(t, http.StatusGone, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"artifact_gone"`)
}
