package controller

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

func VideoProxy(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}
	if !task.ResultRetrievable() || (c.GetInt("token_id") > 0 && c.GetInt("token_id") != task.PrivateData.TokenId) {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}
	if err := service.TokenModelPermissionError(c, service.StoredTaskModelName(task)); err != nil {
		videoProxyError(c, err.StatusCode, "permission_error", err.Error())
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}
	if task.PrivateData.PluginRef != nil {
		artifacts, err := projectTaskArtifacts(task)
		if err != nil {
			writeTaskArtifactProjectionError(c, err)
			return
		}
		for _, artifact := range artifacts {
			if artifact.Type != "video" {
				continue
			}
			adaptor, err := initTaskArtifactAdaptor(c.Request.Context(), task)
			if err != nil {
				writeTaskArtifactProjectionError(c, err)
				return
			}
			provider, ok := adaptor.(relaychannel.TaskContentRequestProvider)
			if !ok {
				writeTaskArtifactProjectionError(c, errTaskArtifactPluginUnavailable)
				return
			}
			descriptor, err := provider.BuildContentRequest(task, artifact.Key, relaychannel.TaskArtifactClientRequest{Method: c.Request.Method, Headers: taskArtifactClientHeaders(c.Request.Header)})
			if err != nil {
				writeTaskArtifactProjectionError(c, err)
				return
			}
			if err := proxyTaskMedia(c, task, descriptor); err != nil {
				writeTaskMediaProxyError(c, err)
			}
			return
		}
		videoProxyError(c, http.StatusNotFound, "artifact_not_found", "Video artifact not found")
		return
	}

	videoProxyError(c, http.StatusServiceUnavailable, "task_state_unavailable", "Task has no immutable plugin execution")
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Cache-Control", "private, max-age=86400")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}
