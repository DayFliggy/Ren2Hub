package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task lists hide provider result URLs and expose plugin provenance only to
// the roles allowed to see it, as upstream does.
func TestTasksToDtoScopesResultAndProvenanceByRole(t *testing.T) {
	newTask := func() *model.Task {
		return &model.Task{
			TaskID: "task_view", Status: model.TaskStatusSuccess, FailReason: "https://cdn.example/legacy.mp4",
			PrivateData: model.TaskPrivateData{
				ResultURL: "https://cdn.example/result.mp4", UpstreamTaskID: "upstream-1", NodeName: "node-a",
				Execution: &model.TaskExecutionSnapshot{RequestID: "req-1", RequestPath: "/v1/tasks/kling",
					TaskPlugin: &model.TaskPluginSnapshot{Key: "kling", Name: "Kling", Version: "1.2.0", APIVersion: 1, Generation: 7,
						Author: &model.TaskPluginAuthorSnapshot{Name: "Vendor", URL: "https://vendor.example"}}},
			},
		}
	}

	user := tasksToDto([]*model.Task{newTask()}, false, common.RoleCommonUser)[0]
	assert.Empty(t, user.ResultURL)
	assert.Empty(t, user.FailReason)
	assert.Nil(t, user.AdminInfo)
	assert.Nil(t, user.RootInfo)

	admin := tasksToDto([]*model.Task{newTask()}, false, common.RoleAdminUser)[0]
	require.NotNil(t, admin.AdminInfo)
	assert.Equal(t, "req-1", admin.AdminInfo.RequestID)
	require.NotNil(t, admin.AdminInfo.TaskPlugin)
	assert.Equal(t, "kling", admin.AdminInfo.TaskPlugin.Key)
	assert.Equal(t, "Vendor", admin.AdminInfo.TaskPlugin.Author.Name)
	assert.Nil(t, admin.RootInfo)

	root := tasksToDto([]*model.Task{newTask()}, false, common.RoleRootUser)[0]
	require.NotNil(t, root.RootInfo)
	assert.Equal(t, "upstream-1", root.RootInfo.UpstreamTaskID)
	assert.EqualValues(t, 7, root.RootInfo.TaskPlugin.Generation)
}
