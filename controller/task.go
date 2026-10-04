package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func GetAllTask(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	// 解析其他查询参数
	queryParams := model.SyncTaskQueryParams{
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ChannelID:      c.Query("channel_id"),
	}

	items := model.TaskGetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllTasks(queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, true, c.GetInt("role")))
	common.ApiSuccess(c, pageInfo)
}

func GetUserTask(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	queryParams := model.SyncTaskQueryParams{
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	items := model.TaskGetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllUserTask(userId, queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, false, common.RoleCommonUser))
	common.ApiSuccess(c, pageInfo)
}

// Task lists never project artifacts or expose a provider result URL; the
// dashboard reads results through GET /api/task/:task_id/artifacts.
func tasksToDto(tasks []*model.Task, fillUser bool, viewerRole int) []*dto.TaskDto {
	var userIdMap map[int]*model.UserBase
	if fillUser {
		userIdMap = make(map[int]*model.UserBase)
		userIds := types.NewSet[int]()
		for _, task := range tasks {
			userIds.Add(task.UserId)
		}
		for _, userId := range userIds.Items() {
			cacheUser, err := model.GetUserCache(userId)
			if err == nil {
				userIdMap[userId] = cacheUser
			}
		}
	}
	result := make([]*dto.TaskDto, len(tasks))
	for i, task := range tasks {
		if fillUser {
			if user, ok := userIdMap[task.UserId]; ok {
				task.Username = user.Username
			}
		}
		item := relay.TaskModel2Dto(task)
		item.LegacyVideoAvailable = legacyVideoAvailable(task)
		item.ResultDiscarded = task.PrivateData.ResultDiscarded
		if task.Status == model.TaskStatusSuccess {
			item.ResultURL = ""
			if taskFailReasonIsLegacyResultURL(task.FailReason) {
				item.FailReason = ""
			}
		}
		snapshot := taskPluginSnapshot(task)
		if viewerRole >= common.RoleAdminUser {
			item.AdminInfo = taskAdminInfo(task, snapshot)
		}
		if viewerRole >= common.RoleRootUser {
			item.RootInfo = taskRootInfo(task, snapshot)
		}
		result[i] = item
	}
	return result
}

func taskPluginSnapshot(task *model.Task) *model.TaskPluginSnapshot {
	if task.PrivateData.Execution == nil {
		return nil
	}
	return task.PrivateData.Execution.TaskPlugin
}

func taskAdminInfo(task *model.Task, snapshot *model.TaskPluginSnapshot) *dto.TaskAdminInfo {
	info := &dto.TaskAdminInfo{}
	if execution := task.PrivateData.Execution; execution != nil {
		info.RequestID, info.RequestPath = execution.RequestID, execution.RequestPath
	}
	if snapshot != nil {
		info.TaskPlugin = &dto.TaskPluginInfo{Key: snapshot.Key, Name: snapshot.Name, Version: snapshot.Version}
		if snapshot.Author != nil {
			info.TaskPlugin.Author = &dto.TaskPluginAuthorInfo{Name: snapshot.Author.Name, URL: snapshot.Author.URL}
		}
	}
	if info.RequestID == "" && info.RequestPath == "" && info.TaskPlugin == nil {
		return nil
	}
	return info
}

func taskRootInfo(task *model.Task, snapshot *model.TaskPluginSnapshot) *dto.TaskRootInfo {
	info := &dto.TaskRootInfo{UpstreamTaskID: task.PrivateData.UpstreamTaskID, NodeName: task.PrivateData.NodeName}
	if snapshot != nil {
		info.TaskPlugin = &dto.TaskPluginRuntimeInfo{Key: snapshot.Key, Version: snapshot.Version, APIVersion: snapshot.APIVersion, Generation: snapshot.Generation}
	}
	if info.TaskPlugin == nil && info.UpstreamTaskID == "" && info.NodeName == "" {
		return nil
	}
	return info
}
