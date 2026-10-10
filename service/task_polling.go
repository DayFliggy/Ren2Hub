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
		if err := failTaskWithoutResult(ctx, task, constant.TaskFailReasonTimedOut); err != nil {
			logger.LogError(ctx, fmt.Sprintf("task timeout release failed (task_id=%s): %v", task.TaskID, err))
		}
	}
}

// failTaskWithoutResult ends a task that never produced a provider result.
// Bill-first releases it: no output reached the user.
func failTaskWithoutResult(ctx context.Context, task *model.Task, reason string) error {
	from := task.Status
	task.Status, task.Progress, task.FailReason = model.TaskStatusFailure, taskcommon.ProgressComplete, reason
	if task.FinishTime == 0 {
		task.FinishTime = time.Now().Unix()
	}
	_, err := completeTaskBilling(ctx, task, from, 0, nil)
	return err
}

// retrySettlementBlockedTasks settles held provider results again without
// polling the provider. A task whose evidence still cannot be verified stays
// blocked and moves to the back of the retry order.
func retrySettlementBlockedTasks(ctx context.Context) {
	tasks, err := model.GetSettlementBlockedTasks(ctx, constant.TaskQueryLimit)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("settlement-blocked task lookup failed: %v", err))
		return
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		var held model.TaskBlockedResult
		if err := common.Unmarshal(task.PrivateData.BlockedResult, &held); err != nil {
			logger.LogError(ctx, fmt.Sprintf("held task result is unreadable (task_id=%s): %v", task.TaskID, err))
			continue
		}
		from := task.Status
		task.Status, task.Progress, task.FailReason, task.FinishTime = held.Status, held.Progress, held.FailReason, held.FinishTime
		task.PrivateData.ResultURL, task.Data = held.ResultURL, held.Data
		var result *relaycommon.TaskInfo
		if usage := task.PrivateData.CompletionUsage; usage != nil {
			result = &relaycommon.TaskInfo{Status: string(held.Status), Url: held.ResultURL, CompletionTokens: usage.CompletionTokens,
				TotalTokens: usage.TotalTokens, UsageFacts: usage.Facts, UsageError: usage.Error}
		}
		if err := settleTaskResult(ctx, task, from, result); err != nil {
			logger.LogError(ctx, fmt.Sprintf("settlement-blocked task retry failed (task_id=%s): %v", task.TaskID, err))
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
	retrySettlementBlockedTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	readyTasks, err := model.TaskSubmissionsReady(ctx, allTasks)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("task submission accounting lookup failed: %v", err))
	}
	for _, t := range readyTasks {
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
				if err := failTaskWithoutResult(ctx, task, constant.TaskFailReasonMissingUpstreamID); err != nil {
					logger.LogError(ctx, fmt.Sprintf("task release failed (task_id=%s): %v", task.TaskID, err))
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

		if task.Status == model.TaskStatusSuccess {
			if err := settleTaskResult(ctx, task, prevStatus, nil); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Suno task %s billing remains pending: %v", task.TaskID, err))
			}
			continue
		}
		var won bool
		var err error
		if task.Status == model.TaskStatusFailure {
			won, err = completeTaskBilling(ctx, task, prevStatus, 0, nil)
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
	batches, singles := groupBatchPolls(ctx, cacheGetChannel, taskIds, taskM)
	requests := len(batches) + len(singles)
	for i := 0; i < requests; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		label := ""
		if i < len(batches) {
			label = fmt.Sprintf("batch of %d", len(batches[i].ids))
			err = updateVideoBatchTasks(ctx, cacheGetChannel, batches[i], taskM)
		} else {
			label = singles[i-len(batches)]
			err = updateVideoSingleTask(ctx, adaptor, cacheGetChannel, label, taskM)
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", label, err.Error()))
		}
		if disablePollingSleep || i == requests-1 {
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
	if err == nil && resp == nil {
		err = errors.New("empty task polling response")
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failTaskPolls(ctx, []*model.Task{task}, pollClassTransport, 0, err)
	}
	responseBody, err := readTaskPollBody(resp)
	if err != nil {
		return failTaskPolls(ctx, []*model.Task{task}, pollClassTransport, resp.StatusCode, err)
	}
	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)
	switch class := classifyPollHTTP(resp.StatusCode); class {
	case pollClassNotFound, pollClassAuth, pollClassTransient:
		return failTaskPolls(ctx, []*model.Task{task}, class, resp.StatusCode, errors.New("provider rejected the poll"))
	}
	outcome, err := parseTaskPollResult(adaptor, task, resp, responseBody)
	if err != nil {
		return failTaskPolls(ctx, []*model.Task{task}, pollClassHookError, resp.StatusCode, err)
	}
	return applyTaskPollResult(ctx, adaptor, task, resp.StatusCode, outcome)
}

// parseTaskPollResult lets the adaptor interpret one provider response; the
// returned payload is what the task stores. Batch adaptors parse a
// single-task request through their batch hook.
func parseTaskPollResult(adaptor TaskPollingAdaptor, task *model.Task, resp *http.Response, responseBody []byte) (*taskPollOutcome, error) {
	if batch, ok := adaptor.(BatchTaskPollingAdaptor); ok && batch.FetchMode() == "batch" {
		results, err := batch.ParseBatchResult([]*model.Task{task}, resp, responseBody)
		if err != nil {
			return nil, err
		}
		result := results[task.GetUpstreamTaskID()]
		if result == nil {
			return nil, errors.New("batch polling omitted requested task")
		}
		return batchTaskOutcome(result, responseBody)
	}
	if pinned, ok := adaptor.(PinnedTaskPollingAdaptor); ok {
		taskResult, err := pinned.ParseTaskResultForTask(task, resp, responseBody)
		if err != nil {
			return nil, err
		}
		return &taskPollOutcome{info: taskResult, data: responseBody}, nil
	}
	// try parse as New API response format
	var responseItems taskdto.TaskResponse[model.Task]
	if err := common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		t := responseItems.Data
		return &taskPollOutcome{info: &relaycommon.TaskInfo{TaskID: t.TaskID, Status: string(t.Status), Url: t.GetResultURL(),
			Progress: t.Progress, Reason: t.FailReason, FinishTime: t.FinishTime}, data: responseBody}, nil
	}
	taskResult, err := adaptor.ParseTaskResult(responseBody)
	if err != nil {
		return nil, err
	}
	return &taskPollOutcome{info: taskResult, data: responseBody}, nil
}

// applyTaskPollResult validates one parsed provider result and advances the
// task. Results the host cannot trust count toward TASK_POLL_MAX_FAILURES
// instead of ending the task at once.
func applyTaskPollResult(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, statusCode int, outcome *taskPollOutcome) error {
	taskResult := outcome.info
	if taskResult == nil || !knownPollStatus(model.TaskStatus(taskResult.Status)) {
		return failTaskPolls(ctx, []*model.Task{task}, pollClassUnrecognized, statusCode, errors.New("task polling response has no recognized job status"))
	}
	if taskResult.TaskID != "" && taskResult.TaskID != task.GetUpstreamTaskID() {
		return failTaskPolls(ctx, []*model.Task{task}, pollClassUnrecognized, statusCode, errors.New("task polling response ID does not match the requested task"))
	}
	terminal := taskResult.Status == model.TaskStatusSuccess || taskResult.Status == model.TaskStatusFailure
	if !terminal && classifyPollHTTP(statusCode) == pollClassOtherClient {
		return failTaskPolls(ctx, []*model.Task{task}, pollClassUnrecognized, statusCode, errors.New("provider error response reported a running job"))
	}
	snap := task.Snapshot()
	if outcome.submitTime > 0 {
		task.SubmitTime = outcome.submitTime
	}
	if outcome.startTime > 0 {
		task.StartTime = outcome.startTime
	}
	if outcome.data != nil {
		task.Data = redactVideoResponseBody(outcome.data)
	}
	if taskResult.PluginState != nil {
		task.PrivateData.PluginState = taskResult.PluginState
	}
	if !terminal {
		task.PrivateData.PollFailures = 0
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
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", task.TaskID), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
	}
	// Only a terminal status completes a task; a provider's "100%" on a
	// running job keeps the status-derived progress.
	if taskResult.Progress != "" && (terminal || taskResult.Progress != taskcommon.ProgressComplete) {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone {
		if taskResult.FinishTime > 0 {
			task.FinishTime = taskResult.FinishTime
		}
		task.PrivateData.CompletionUsage = &model.TaskCompletionUsage{CompletionTokens: taskResult.CompletionTokens, TotalTokens: taskResult.TotalTokens, Facts: taskResult.UsageFacts, Error: taskResult.UsageError}
		return settleTaskResult(ctx, task, snap.Status, taskResult)
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

const (
	pollClassOK           = "ok"
	pollClassOtherClient  = "other_client"
	pollClassNotFound     = "not_found"
	pollClassAuth         = "auth"
	pollClassTransient    = "transient"
	pollClassUnrecognized = "unrecognized"
	pollClassHookError    = "hook_error"
	pollClassTransport    = "transport_error"
)

// taskPollOutcome is one parsed provider result plus the payload the task
// stores; nil data keeps the stored payload.
type taskPollOutcome struct {
	info                  *relaycommon.TaskInfo
	data                  []byte
	submitTime, startTime int64
}

func batchTaskOutcome(result *BatchTaskResult, fallback []byte) (*taskPollOutcome, error) {
	info := result.TaskInfo
	if result.FinishTime > 0 {
		info.FinishTime = result.FinishTime
	}
	outcome := &taskPollOutcome{info: &info, data: fallback, submitTime: result.SubmitTime, startTime: result.StartTime}
	if result.Data != nil {
		data, err := common.Marshal(result.Data)
		if err != nil {
			return nil, err
		}
		outcome.data = data
	}
	return outcome, nil
}

func readTaskPollBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, errors.New("task polling response exceeds 8 MiB")
	}
	return body, nil
}

func classifyPollHTTP(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return pollClassOK
	case statusCode == http.StatusNotFound || statusCode == http.StatusGone:
		return pollClassNotFound
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return pollClassAuth
	case statusCode == http.StatusTooManyRequests || statusCode >= 500:
		return pollClassTransient
	case statusCode >= 400 && statusCode < 500:
		return pollClassOtherClient
	default:
		return pollClassTransient
	}
}

func knownPollStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure:
		return true
	default:
		return false
	}
}

// pollFailureLogText names a failed poll for the server log; the stored fail
// reason of a task given up is a fixed sentence without class or status.
func pollFailureLogText(class string, statusCode int) string {
	if statusCode > 0 {
		return fmt.Sprintf("poll failed: %s (HTTP %d)", class, statusCode)
	}
	return "poll failed: " + class
}

// failTaskPolls applies one failed poll to each task and returns the failure
// for the caller's log. A missing upstream task ends at once; other classes
// count toward TASK_POLL_MAX_FAILURES (0 disables the cutoff, leaving the
// timeout sweep). Both endings release the reservation: no output reached
// the user. The stored reason is a fixed sentence from
// constant.TaskFailReasons; the class, the HTTP status and the provider
// detail, which can carry URLs or credentials, stay in the returned error for
// server logs only.
func failTaskPolls(ctx context.Context, tasks []*model.Task, class string, statusCode int, cause error) error {
	// The host may cancel a polling pass when its system-task lease is lost or
	// the runner is shutting down. That is control flow, not a provider poll
	// failure: leave the task's consecutive-failure counter untouched so the
	// next owner can retry it normally.
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	reason := constant.TaskFailReasonPollGaveUp
	if class == pollClassNotFound {
		reason = constant.TaskFailReasonUpstreamNotFound
	}
	var result error
	for _, task := range tasks {
		if class != pollClassNotFound {
			task.PrivateData.PollFailures++
		}
		if class == pollClassNotFound || (constant.TaskPollMaxFailures > 0 && task.PrivateData.PollFailures >= constant.TaskPollMaxFailures) {
			result = errors.Join(result, failTaskWithoutResult(ctx, task, reason))
			continue
		}
		if _, err := task.UpdateWithStatus(task.Status); err != nil {
			result = errors.Join(result, err)
		}
	}
	return errors.Join(fmt.Errorf("%s: %s", pollFailureLogText(class, statusCode), common.MaskSensitiveInfo(cause.Error())), result)
}

type batchPollGroup struct {
	adaptor BatchTaskPollingAdaptor
	key     string
	ids     []string
}

// groupBatchPolls gathers plugin tasks whose pinned plugin polls in batch
// mode and that share one plugin version and credential, so each group is
// one provider request. Everything else polls one task at a time.
func groupBatchPolls(ctx context.Context, ch *model.Channel, taskIds []string, taskM map[string]*model.Task) ([]*batchPollGroup, []string) {
	var groups []*batchPollGroup
	index := make(map[string]*batchPollGroup)
	var singles []string
	for _, taskID := range taskIds {
		task := taskM[taskID]
		adaptor, key, ok := batchPollTarget(ctx, ch, task)
		if !ok {
			singles = append(singles, taskID)
			continue
		}
		ref := task.PrivateData.PluginRef
		groupKey := ref.Key + "@" + ref.Version + ":" + ref.SourceHash + "\x00" + key
		group := index[groupKey]
		if group == nil {
			group = &batchPollGroup{adaptor: adaptor, key: key}
			index[groupKey] = group
			groups = append(groups, group)
		}
		group.ids = append(group.ids, taskID)
	}
	batches := make([]*batchPollGroup, 0, len(groups))
	for _, group := range groups {
		if len(group.ids) == 1 {
			singles = append(singles, group.ids[0])
			continue
		}
		batches = append(batches, group)
	}
	return batches, singles
}

// batchPollTarget resolves a task's pinned batch adaptor and credential.
// Resolution errors send the task to the single-task path, which reports them.
func batchPollTarget(ctx context.Context, ch *model.Channel, task *model.Task) (BatchTaskPollingAdaptor, string, bool) {
	if task == nil || task.PrivateData.PluginRef == nil || GetPinnedTaskAdaptorFunc == nil {
		return nil, "", false
	}
	pinned, err := GetPinnedTaskAdaptorFunc(task)
	if err != nil {
		return nil, "", false
	}
	batch, ok := pinned.(BatchTaskPollingAdaptor)
	if !ok || batch.FetchMode() != "batch" {
		return nil, "", false
	}
	key, _, err := model.ResolveTaskCredential(ctx, ch, task)
	if err != nil {
		return nil, "", false
	}
	return batch, key, true
}

// updateVideoBatchTasks polls one batch group with a single provider request.
// A request-level failure applies to every task in the group, as upstream.
func updateVideoBatchTasks(ctx context.Context, ch *model.Channel, group *batchPollGroup, taskM map[string]*model.Task) error {
	current, err := model.GetChannelById(ch.Id, true)
	if err != nil {
		return err
	}
	baseURL := constant.ChannelBaseURLs[current.Type]
	if current.GetBaseURL() != "" {
		baseURL = current.GetBaseURL()
	}
	tasks := make([]*model.Task, 0, len(group.ids))
	for _, id := range group.ids {
		tasks = append(tasks, taskM[id])
	}
	adaptor := group.adaptor
	if aware, ok := adaptor.(interface{ SetTaskContext(context.Context) }); ok {
		aware.SetTaskContext(ctx)
	}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId: current.Id, ChannelType: current.Type, ChannelBaseUrl: baseURL,
		ApiKey: group.key, ChannelSetting: current.GetSetting(),
	}})
	resp, err := adaptor.FetchBatchTasks(baseURL, group.key, tasks, current.GetSetting().Proxy)
	if err == nil && resp == nil {
		err = errors.New("empty task polling response")
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failTaskPolls(ctx, tasks, pollClassTransport, 0, err)
	}
	responseBody, err := readTaskPollBody(resp)
	if err != nil {
		return failTaskPolls(ctx, tasks, pollClassTransport, resp.StatusCode, err)
	}
	switch class := classifyPollHTTP(resp.StatusCode); class {
	case pollClassNotFound, pollClassAuth, pollClassTransient:
		return failTaskPolls(ctx, tasks, class, resp.StatusCode, errors.New("provider rejected the poll"))
	}
	results, err := adaptor.ParseBatchResult(tasks, resp, responseBody)
	if err != nil {
		return failTaskPolls(ctx, tasks, pollClassHookError, resp.StatusCode, err)
	}
	var result error
	for _, task := range tasks {
		item := results[task.GetUpstreamTaskID()]
		if item == nil {
			result = errors.Join(result, failTaskPolls(ctx, []*model.Task{task}, pollClassUnrecognized, resp.StatusCode, errors.New("batch polling omitted requested task")))
			continue
		}
		// The shared body holds other tasks' results, so a task without its
		// own data keeps what it stored.
		outcome, err := batchTaskOutcome(item, nil)
		if err == nil {
			err = applyTaskPollResult(ctx, adaptor, task, resp.StatusCode, outcome)
		}
		result = errors.Join(result, err)
	}
	return result
}
