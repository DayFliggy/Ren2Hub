package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// midjourneyPollSummary is the result recorded on a midjourney_poll system task
// row, summarizing one polling pass.
type midjourneyPollSummary struct {
	UnfinishedTasks int `json:"unfinished_tasks"`
	ChannelsScanned int `json:"channels_scanned"`
	NullTasksFailed int `json:"null_tasks_failed"`
}

// runMidjourneyTaskUpdateOnce performs one Midjourney polling pass synchronously.
// It honors ctx cancellation (the system-task runner cancels it when the lease
// is lost) and, when report is non-nil, reports progress as (processedChannels,
// totalChannels) so the system task surfaces a percentage.
func runMidjourneyTaskUpdateOnce(ctx context.Context, report func(processed, total int)) (midjourneyPollSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tasks, err := model.GetAllUnFinishTasks(ctx)
	if err != nil {
		return midjourneyPollSummary{}, err
	}
	summary := midjourneyPollSummary{UnfinishedTasks: len(tasks)}
	var pollErrors []error
	channels := make(map[int][]*model.Midjourney)
	for _, task := range tasks {
		ready, err := model.MidjourneySubmissionReady(ctx, task)
		if err != nil || !ready {
			if err != nil {
				pollErrors = append(pollErrors, fmt.Errorf("Midjourney submission accounting (row_id=%d): %w", task.Id, err))
			}
			continue
		}
		if task.SettlementBlock != "" {
			// The provider result is held; settle it again instead of polling,
			// and never release it as unfinished.
			if err := retryBlockedMidjourney(ctx, task); err != nil {
				pollErrors = append(pollErrors, fmt.Errorf("settlement-blocked Midjourney task (row_id=%d): %w", task.Id, err))
			}
			continue
		}
		if reason := midjourneyUnfinishableReason(task); reason != "" {
			// No result reached the user, so bill-first releases the job.
			from := task.Status
			task.Status, task.Progress, task.FailReason = "FAILURE", "100%", reason
			if task.FinishTime == 0 {
				task.FinishTime = time.Now().UnixMilli()
			}
			if won, err := service.CompleteMidjourneyBilling(ctx, task, from); err != nil {
				pollErrors = append(pollErrors, fmt.Errorf("release unfinished Midjourney task (row_id=%d): %w", task.Id, err))
			} else if won {
				summary.NullTasksFailed++
			}
			continue
		}
		channels[task.ChannelId] = append(channels[task.ChannelId], task)
	}
	var workers sync.WaitGroup
	results := make(chan error, len(channels))
	for id, group := range channels {
		if ctx.Err() != nil {
			break
		}
		workers.Add(1)
		go func(id int, group []*model.Midjourney) {
			defer workers.Done()
			results <- pollMidjourneyChannel(ctx, id, group)
		}(id, group)
	}
	go func() { workers.Wait(); close(results) }()
	for err := range results {
		if err != nil {
			pollErrors = append(pollErrors, err)
		}
		summary.ChannelsScanned++
		if report != nil {
			report(summary.ChannelsScanned, len(channels))
		}
	}
	return summary, errors.Join(append(pollErrors, ctx.Err())...)
}

// midjourneyUnfinishableReason uses the same timeout as other async tasks.
// Midjourney submit times are Unix milliseconds.
func midjourneyUnfinishableReason(task *model.Midjourney) string {
	if task.MjId == "" {
		return "missing upstream task identifier"
	}
	if constant.TaskTimeoutMinutes > 0 && task.SubmitTime > 0 &&
		time.Now().UnixMilli()-task.SubmitTime > int64(constant.TaskTimeoutMinutes)*60*1000 {
		return "任务超时，未返回结果"
	}
	return ""
}

func pollMidjourneyChannel(ctx context.Context, channelID int, tasks []*model.Midjourney) error {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return fmt.Errorf("Midjourney channel unavailable (channel_id=%d): %w", channelID, err)
	}
	var pollErrors []error
	groups := make(map[string][]*model.Midjourney)
	for _, task := range tasks {
		if _, _, err := model.ResolveMidjourneyCredential(ctx, channel, task); err != nil {
			pollErrors = append(pollErrors, fmt.Errorf("Midjourney task owner unavailable (row_id=%d): %w", task.Id, err))
			continue
		}
		groups[task.PrivateData.CredentialID] = append(groups[task.PrivateData.CredentialID], task)
	}
	for _, group := range groups {
		if ctx.Err() != nil {
			return errors.Join(append(pollErrors, ctx.Err())...)
		}
		if err := pollMidjourneyCredential(ctx, channelID, group); err != nil {
			pollErrors = append(pollErrors, fmt.Errorf("Midjourney poll remains pending (channel_id=%d): %w", channelID, err))
		}
	}
	return errors.Join(pollErrors...)
}

func pollMidjourneyCredential(ctx context.Context, channelID int, tasks []*model.Midjourney) error {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return err
	}
	key, _, err := model.ResolveMidjourneyCredential(ctx, channel, tasks[0])
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(tasks))
	owners := make(map[string][]*model.Midjourney)
	for _, task := range tasks {
		resolved, _, err := model.ResolveMidjourneyCredential(ctx, channel, task)
		if err != nil || resolved != key {
			return model.ErrGatewayStateUnavailable
		}
		if len(owners[task.MjId]) == 0 {
			ids = append(ids, task.MjId)
		}
		owners[task.MjId] = append(owners[task.MjId], task)
	}
	body, err := common.Marshal(map[string]any{"ids": ids})
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(channel.GetBaseURL(), "/")+"/mj/task/list-by-condition", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("mj-api-secret", key)
	channelSetting := channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(channelSetting.Proxy, channelSetting)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("Midjourney polling transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Midjourney polling HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 16*1024*1024 {
		return errors.New("Midjourney polling response too large")
	}
	var updates []dto.MidjourneyDto
	if err := common.Unmarshal(data, &updates); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, update := range updates {
		if update.MjId == "" || seen[update.MjId] {
			return errors.New("ambiguous Midjourney polling response")
		}
		seen[update.MjId] = true
	}
	for _, update := range updates {
		for _, task := range owners[update.MjId] {
			if err := applyMidjourneyTaskUpdate(ctx, task, update); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyMidjourneyTaskUpdate(ctx context.Context, task *model.Midjourney, update dto.MidjourneyDto) error {
	switch update.Status {
	case "NOT_START", "SUBMITTED", "IN_PROGRESS", "QUEUED", "SUCCESS", "FAILURE":
	default:
		// Polling errors and incomplete envelopes do not prove the job failed.
		return errors.New("Midjourney response has no recognized task status")
	}
	from := task.Status
	next := *task
	next.Code, next.Status, next.Progress = 1, update.Status, update.Progress
	next.PromptEn, next.State, next.FailReason = update.PromptEn, update.State, update.FailReason
	if update.SubmitTime > 0 {
		next.SubmitTime = update.SubmitTime
	}
	next.StartTime, next.FinishTime = update.StartTime, update.FinishTime
	next.ImageUrl, next.VideoUrl = update.ImageUrl, update.VideoUrl
	properties, err := common.Marshal(update.Properties)
	if err != nil {
		return err
	}
	buttons, err := common.Marshal(update.Buttons)
	if err != nil {
		return err
	}
	videos, err := common.Marshal(update.VideoUrls)
	if err != nil {
		return err
	}
	next.Properties, next.Buttons, next.VideoUrls = string(properties), string(buttons), string(videos)
	if next.Status == "SUCCESS" || next.Status == "FAILURE" {
		next.Progress = "100%"
		won, err := service.CompleteMidjourneyBilling(ctx, &next, from)
		if !won && next.Status == "SUCCESS" && model.IsBillingEvidenceUnverifiable(err) {
			return holdBlockedMidjourney(ctx, task, from, update, err)
		}
		return err
	}
	// Status guards the provider update while financial and ownership metadata
	// remain exclusively managed by their transaction paths.
	return model.DB.WithContext(ctx).Model(&model.Midjourney{}).Where("id = ? AND status = ?", next.Id, from).
		Updates(map[string]any{"code": next.Code, "status": next.Status, "progress": next.Progress,
			"prompt_en": next.PromptEn, "state": next.State, "fail_reason": next.FailReason,
			"submit_time": next.SubmitTime, "start_time": next.StartTime, "finish_time": next.FinishTime,
			"image_url": next.ImageUrl, "video_url": next.VideoUrl, "video_urls": next.VideoUrls,
			"buttons": next.Buttons, "properties": next.Properties}).Error
}

// holdBlockedMidjourney keeps a successful provider update whose settlement
// evidence cannot be verified on the job, with its reservation held, so the
// next polling pass settles it again instead of releasing it as unfinished.
func holdBlockedMidjourney(ctx context.Context, task *model.Midjourney, from string, update dto.MidjourneyDto, cause error) error {
	held, err := common.Marshal(update)
	if err != nil {
		return errors.Join(cause, err)
	}
	newly, err := model.PersistMidjourneySettlementBlock(ctx, task, from, held)
	if err != nil {
		return errors.Join(cause, err)
	}
	if newly {
		logger.LogError(ctx, fmt.Sprintf("Midjourney settlement blocked, result held (row_id=%d, reason=%s): %v", task.Id, model.TaskSettlementBlockedEvidence, cause))
	}
	return nil
}

func retryBlockedMidjourney(ctx context.Context, task *model.Midjourney) error {
	var update dto.MidjourneyDto
	if err := common.Unmarshal(task.PrivateData.BlockedResult, &update); err != nil {
		return fmt.Errorf("held Midjourney result is unreadable: %w", err)
	}
	return applyMidjourneyTaskUpdate(ctx, task, update)
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = service.BuildMidjourneyTaskImageURL(system_setting.ServerAddress, midjourney.Id)
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllUserTask(userId, queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = service.BuildMidjourneyTaskImageURL(system_setting.ServerAddress, midjourney.Id)
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
