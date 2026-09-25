package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

type PinnedTaskPollingAdaptor interface {
	TaskPollingAdaptor
	FetchTaskForTask(baseURL, key string, task *model.Task, proxy string) (*http.Response, error)
	ParseTaskResultForTask(task *model.Task, resp *http.Response, body []byte) (*relaycommon.TaskInfo, error)
}

type BatchTaskPollingAdaptor interface {
	TaskPollingAdaptor
	FetchMode() string
	FetchBatchTasks(string, string, []*model.Task, string) (*http.Response, error)
	ParseBatchResult([]*model.Task, *http.Response, []byte) (map[string]*BatchTaskResult, error)
}

var GetPinnedTaskAdaptorFunc func(*model.Task) (TaskPollingAdaptor, error)

type BatchTaskResult struct {
	TaskInfo   relaycommon.TaskInfo
	Action     string
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	Data       any
}

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	for _, task := range tasks {
		if err := model.HoldTaskBilling(ctx, task, task.Status, "task_timeout_outcome_unknown"); err != nil {
			logger.LogError(ctx, fmt.Sprintf("task timeout reconciliation failed (task_id=%s): %v", task.TaskID, err))
		}
	}

}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// RunTaskPollingOnce performs one async-task (Suno/video) polling pass
// synchronously. It honors ctx cancellation (the system-task runner cancels it
// when the lease is lost) and, when report is non-nil, reports progress as
// (processedPlatforms, totalPlatforms). It returns immediately if the task
// adaptor factory has not been wired yet, to avoid a nil call during startup.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if GetTaskAdaptorFunc == nil {
		return summary
	}
	if ctx == nil {
		ctx = context.Background()
	}

	common.SysLog("任务进度轮询开始")
	if err := model.RecoverTaskResultMetrics(ctx, constant.TaskQueryLimit); err != nil {
		logger.LogError(ctx, fmt.Sprintf("task result metrics recovery failed: %v", err))
	}
	sweepTimedOutTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		ready, err := model.TaskSubmissionReady(ctx, t)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("task submission accounting lookup failed (task_id=%s): %v", t.TaskID, err))
		}
		if !ready {
			continue
		}
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		// Upstream IDs are scoped to an account/endpoint, not globally unique.
		type owner struct {
			channelID, userID, tokenID, keyIndex int
			credentialID, endpointID, legacyKey  string
		}
		owners := make(map[owner][]*model.Task)
		for _, task := range tasks {
			if task.GetUpstreamTaskID() == "" {
				if err := model.HoldTaskBilling(ctx, task, task.Status, "task_upstream_identity_missing"); err != nil {
					logger.LogError(ctx, fmt.Sprintf("task reconciliation failed (task_id=%s): %v", task.TaskID, err))
				}
				continue
			}
			binding := task.PrivateData
			identity := owner{channelID: task.ChannelId, userID: task.UserId, tokenID: binding.TokenId,
				credentialID: binding.CredentialID, endpointID: binding.EndpointID}
			if binding.CredentialVersion == 0 {
				identity.keyIndex = binding.ChannelKeyIndex
				identity.legacyKey = binding.Key
			}
			owners[identity] = append(owners[identity], task)
		}
		if platform != constant.TaskPlatformSuno {
			taskChannelM := make(map[int][]string)
			taskM := make(map[string]*model.Task)
			for _, task := range tasks {
				if task.GetUpstreamTaskID() == "" {
					continue
				}
				// Video fetches resolve a credential per task. Index by our row ID
				// so colliding provider IDs do not fragment per-channel pacing.
				lookupID := fmt.Sprint(task.ID)
				taskM[lookupID] = task
				taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], lookupID)
			}
			DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
			continue
		}
		// Preserve per-channel pacing while unrelated channels progress in parallel.
		channelOwners := make(map[int][][]*model.Task)
		for identity, ownedTasks := range owners {
			channelOwners[identity.channelID] = append(channelOwners[identity.channelID], ownedTasks)
		}
		var wg sync.WaitGroup
		for _, groups := range channelOwners {
			wg.Add(1)
			gopool.Go(func() {
				defer wg.Done()
				for _, ownedTasks := range groups {
					if ctx.Err() != nil {
						return
					}
					taskChannelM := make(map[int][]string)
					taskM := make(map[string]*model.Task)
					for _, task := range ownedTasks {
						upstreamID := task.GetUpstreamTaskID()
						taskM[upstreamID] = task
						taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
					}
					DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
				}
			})
		}
		wg.Wait()
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog("任务进度轮询完成")
	return summary
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.GetChannelById(channelId, true)
	if err != nil {
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	groups := make(map[string]map[string]*model.Task)
	var result error
	for _, taskID := range taskIds {
		task := taskM[taskID]
		if task == nil || task.ChannelId != channelId {
			result = errors.Join(result, model.ErrGatewayStateUnavailable)
			continue
		}
		key, _, err := model.ResolveTaskCredential(ctx, ch, task)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if groups[key] == nil {
			groups[key] = make(map[string]*model.Task)
		}
		groups[key][taskID] = task
	}
	for key, tasks := range groups {
		result = errors.Join(result, pollSunoCredentialGroup(ctx, adaptor, ch, key, tasks))
	}
	return result
}

func pollSunoCredentialGroup(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, key string, taskM map[string]*model.Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	taskIDs := make([]string, 0, len(taskM))
	for id := range taskM {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	resp, err := adaptor.FetchTask(ch.GetBaseURL(), key, map[string]any{"ids": taskIDs}, ch.GetSetting().Proxy)
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("empty Suno polling response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems taskdto.TaskResponse[[]taskdto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		return fmt.Errorf("Suno polling rejected: %s", responseItems.Code)
	}

	for _, responseItem := range responseItems.Data {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		prevStatus := task.Status
		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		isFailure := responseItem.FailReason != "" || task.Status == model.TaskStatusFailure
		if isFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Status = model.TaskStatusFailure
			task.Progress = "100%"
		}
		if responseItem.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		var won bool
		var err error
		if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
			var actualQuota common.Quota
			if !isFailure {
				actualQuota, _, _, err = taskCompletionQuota(nil, task, nil)
				if err != nil {
					err = errors.Join(err, model.HoldTaskBilling(ctx, task, prevStatus, "task_final_usage_invalid"))
					logger.LogError(ctx, fmt.Sprintf("Suno task %s requires billing reconciliation: %v", task.TaskID, err))
					continue
				}
			}
			won, err = completeTaskBilling(ctx, task, prevStatus, actualQuota, task.FailReason)
		} else {
			won, err = task.UpdateWithStatus(prevStatus)
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateSunoTask task %s error: %v", task.TaskID, err))
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s CAS lost or no-op update, skip billing", task.TaskID))
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask taskdto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	return !bytes.Equal(oldTask.Data, newTask.Data)
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return fmt.Errorf("task channel unavailable: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil && GetPinnedTaskAdaptorFunc == nil {
		return fmt.Errorf("video adaptor not found")
	}
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	current, err := model.GetChannelById(ch.Id, true)
	if err != nil {
		return err
	}
	ch = current
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	if task.PrivateData.PluginRef != nil {
		if GetPinnedTaskAdaptorFunc == nil {
			return errors.New("plugin task resolver unavailable")
		}
		adaptor, err = GetPinnedTaskAdaptorFunc(task)
		if err != nil {
			return err
		}
	}
	if adaptor == nil {
		return errors.New("task adaptor unavailable")
	}
	if aware, ok := adaptor.(interface{ SetTaskContext(context.Context) }); ok {
		aware.SetTaskContext(ctx)
	}
	key, _, err := model.ResolveTaskCredential(ctx, ch, task)
	if err != nil {
		return err
	}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId: ch.Id, ChannelType: ch.Type, ChannelBaseUrl: baseURL,
		ApiKey: key, ChannelSetting: ch.GetSetting(), UpstreamModelName: task.Properties.UpstreamModelName,
	}})
	var resp *http.Response
	if pinned, ok := adaptor.(PinnedTaskPollingAdaptor); ok {
		resp, err = pinned.FetchTaskForTask(baseURL, key, task, proxy)
	} else {
		resp, err = adaptor.FetchTask(baseURL, key, map[string]any{"task_id": task.GetUpstreamTaskID(), "action": task.Action}, proxy)
	}
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	if resp == nil {
		return errors.New("empty task polling response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("task polling returned HTTP %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}
	if len(responseBody) > 8<<20 {
		return errors.New("task polling response exceeds 8 MiB")
	}

	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems taskdto.TaskResponse[model.Task]
	if batch, ok := adaptor.(BatchTaskPollingAdaptor); ok && batch.FetchMode() == "batch" {
		results, parseErr := batch.ParseBatchResult([]*model.Task{task}, resp, responseBody)
		if parseErr != nil {
			return parseErr
		}
		result := results[task.GetUpstreamTaskID()]
		if result == nil {
			return errors.New("batch polling omitted requested task")
		}
		taskResult = &result.TaskInfo
		if result.SubmitTime > 0 {
			task.SubmitTime = result.SubmitTime
		}
		if result.StartTime > 0 {
			task.StartTime = result.StartTime
		}
		if result.FinishTime > 0 {
			taskResult.FinishTime = result.FinishTime
		}
		if result.Data != nil {
			if data, encodeErr := common.Marshal(result.Data); encodeErr == nil {
				responseBody = data
			} else {
				return encodeErr
			}
		}
	} else if pinned, ok := adaptor.(PinnedTaskPollingAdaptor); ok {
		taskResult, err = pinned.ParseTaskResultForTask(task, resp, responseBody)
		if err != nil {
			return err
		}
	} else if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		taskResult.FinishTime = t.FinishTime
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}

	if taskResult == nil || taskResult.Status == "" {
		return errors.New("task polling response has no explicit job status")
	}
	if taskResult.TaskID != "" && taskResult.TaskID != task.GetUpstreamTaskID() {
		return errors.New("task polling response ID does not match the requested task")
	}
	task.Data = redactVideoResponseBody(responseBody)
	if taskResult.PluginState != nil {
		task.PrivateData.PluginState = taskResult.PluginState
	}

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case string(model.TaskStatusNotStart), model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone {
		if taskResult.FinishTime > 0 {
			task.FinishTime = taskResult.FinishTime
		}
		task.PrivateData.CompletionUsage = &model.TaskCompletionUsage{CompletionTokens: taskResult.CompletionTokens, TotalTokens: taskResult.TotalTokens, Facts: taskResult.UsageFacts, Error: taskResult.UsageError}
		quota, reason, clamp, err := taskCompletionQuota(adaptor, task, taskResult)
		if err != nil {
			return errors.Join(err, model.HoldTaskBilling(ctx, task, snap.Status, "task_final_usage_invalid"))
		}
		_, err = completeTaskBilling(ctx, task, snap.Status, quota, reason, clamp)
		return err
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}
