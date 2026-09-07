package service

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func CoverTaskActionToModelName(platform constant.TaskPlatform, action string) string {
	return strings.ToLower(string(platform)) + "_" + strings.ToLower(action)
}

// StoredTaskModelName returns the model identity used for authorization of a
// persisted task, including records created before OriginModelName was stored.
func StoredTaskModelName(task *model.Task) string {
	if task == nil {
		return ""
	}
	if name := strings.TrimSpace(task.Properties.OriginModelName); name != "" {
		return name
	}
	if name := strings.TrimSpace(task.Properties.UpstreamModelName); name != "" {
		return name
	}
	return CoverTaskActionToModelName(task.Platform, task.Action)
}
