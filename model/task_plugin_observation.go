package model

import (
	"context"
	"github.com/QuantumNous/new-api/constant"
)

func GetByTaskIdsForPlatforms(userID int, platforms []constant.TaskPlatform, ids []string) ([]*Task, error) {
	var tasks []*Task
	err := DB.Where("user_id = ? AND platform IN ? AND task_id IN ?", userID, platforms, ids).Find(&tasks).Error
	return tasks, err
}

func GetUniqueByOnlyTaskId(id string) (*Task, bool, error) {
	var tasks []Task
	err := DB.Where("task_id = ?", id).Limit(2).Find(&tasks).Error
	if err != nil || len(tasks) != 1 {
		return nil, false, err
	}
	return &tasks[0], true, nil
}

func GetByOnlyTaskId(id string) (*Task, bool, error) { return GetUniqueByOnlyTaskId(id) }

func GetTaskForProtocolObservation(ctx context.Context, userID int, platform constant.TaskPlatform, taskID string) (*Task, bool, error) {
	if taskID == "" {
		return nil, false, nil
	}
	var task Task
	err := DB.WithContext(ctx).
		Where("user_id = ? AND platform = ? AND task_id = ?", userID, platform, taskID).
		First(&task).Error
	exists, err := RecordExist(err)
	if err != nil || !exists {
		return nil, exists, err
	}
	return &task, true, nil
}
