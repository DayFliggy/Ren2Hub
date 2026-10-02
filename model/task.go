package model

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
)

type TaskStatus string

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusNotStart, TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart   TaskStatus = "NOT_START"
	TaskStatusSubmitted             = "SUBMITTED"
	TaskStatusQueued                = "QUEUED"
	TaskStatusInProgress            = "IN_PROGRESS"
	TaskStatusFailure               = "FAILURE"
	TaskStatusSuccess               = "SUCCESS"
	TaskStatusUnknown               = "UNKNOWN"
)

// TaskRefundLegacyCutoff separates tasks created before timeout refunds were
// introduced. Those legacy tasks are failed without an automatic refund.
const TaskRefundLegacyCutoff int64 = 1771718400 // 2026-02-22 00:00:00 UTC

type Task struct {
	ID         int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt  int64                 `json:"created_at" gorm:"index"`
	UpdatedAt  int64                 `json:"updated_at"`
	TaskID     string                `json:"task_id" gorm:"type:varchar(191);index"` // 第三方id，不一定有/ song id\ Task id
	Platform   constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId     int                   `json:"user_id" gorm:"index"`
	Group      string                `json:"group" gorm:"type:varchar(50)"` // 兼容历史任务记录；不参与统一路由计费
	ChannelId  int                   `json:"channel_id" gorm:"index"`
	Quota      common.Quota          `json:"quota" gorm:"type:bigint"`
	Action     string                `json:"action" gorm:"type:varchar(40);index"` // 任务类型, song, lyrics, description-mode
	Status     TaskStatus            `json:"status" gorm:"type:varchar(20);index"` // 任务状态
	FailReason string                `json:"fail_reason"`
	SubmitTime int64                 `json:"submit_time" gorm:"index"`
	StartTime  int64                 `json:"start_time" gorm:"index"`
	FinishTime int64                 `json:"finish_time" gorm:"index"`
	Progress   string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties Properties            `json:"properties" gorm:"type:json"`
	Username   string                `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	*m = Properties{}
	switch value := val.(type) {
	case nil:
		return nil
	case []byte:
		if len(value) == 0 {
			return nil
		}
		return common.Unmarshal(value, m)
	case string:
		if value == "" {
			return nil
		}
		return common.UnmarshalJsonStr(value, m)
	default:
		return fmt.Errorf("unsupported task properties database value %T", val)
	}
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	data, err := common.Marshal(m)
	return string(data), err
}

type TaskPrivateData struct {
	// Key remains only to read tasks created before credential references were
	// introduced. New tasks must resolve credentials from their channel.
	Key                 string                   `json:"key,omitempty"`
	ChannelKeyIndex     int                      `json:"channel_key_index,omitempty"`
	CredentialVersion   int                      `json:"credential_version,omitempty"`
	CredentialID        string                   `json:"credential_id,omitempty"`
	EndpointID          string                   `json:"endpoint_id,omitempty"`
	UpstreamTaskID      string                   `json:"upstream_task_id,omitempty"` // 上游真实 task ID
	SubmissionRequestID string                   `json:"submission_request_id,omitempty"`
	ResultURL           string                   `json:"result_url,omitempty"` // 任务成功后的结果 URL（视频地址等）
	PluginRef           *hosttypes.TaskPluginRef `json:"plugin_ref,omitempty"`
	PluginAttemptID     string                   `json:"plugin_attempt_id,omitempty"`
	Execution           *TaskExecutionSnapshot   `json:"execution,omitempty"`
	ResponsesBackground bool                     `json:"responses_background,omitempty"`
	PluginState         json.RawMessage          `json:"plugin_state,omitempty"`
	PollFailures        int                      `json:"poll_failures,omitempty"`
	ResultDiscarded     bool                     `json:"result_discarded,omitempty"`
	CompletionUsage     *TaskCompletionUsage     `json:"completion_usage,omitempty"`
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource  string              `json:"billing_source,omitempty"`  // 新任务使用 wallet；保留历史来源供结算时校验
	TokenId        int                 `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	NodeName       string              `json:"node_name,omitempty"`       // 发起任务的节点名，轮询结算阶段据此归属日志而非最后查询节点
	BillingContext *TaskBillingContext `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
}

// TaskCompletionUsage retains provider evidence even if pricing cannot settle it.
type TaskCompletionUsage struct {
	CompletionTokens int            `json:"completion_tokens"`
	TotalTokens      int            `json:"total_tokens"`
	Facts            map[string]any `json:"facts,omitempty"`
	Error            string         `json:"error,omitempty"`
}

type TaskExecutionSnapshot struct {
	RequestID   string              `json:"request_id,omitempty"`
	RequestPath string              `json:"request_path,omitempty"`
	TaskPlugin  *TaskPluginSnapshot `json:"task_plugin,omitempty"`
}

type TaskPluginSnapshot struct {
	Key        string                    `json:"key"`
	Name       string                    `json:"name"`
	Version    string                    `json:"version"`
	SourceHash string                    `json:"source_hash"`
	Author     *TaskPluginAuthorSnapshot `json:"author,omitempty"`
	APIVersion int                       `json:"api_version"`
	Generation uint64                    `json:"generation"`
}

type TaskPluginAuthorSnapshot struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

func (t *Task) ResultRetrievable() bool { return t != nil && !t.PrivateData.ResultDiscarded }

// Clear discarded payloads before every database write, including an INSERT
// inside an uncommitted terminal transaction. Keep the caller's response and
// the result hash input intact; removing fields only in a later UPDATE would
// still put the payload in database journals.
func taskResultForStorage(data json.RawMessage, privateData TaskPrivateData) (json.RawMessage, TaskPrivateData) {
	if privateData.ResultDiscarded {
		data = nil
		privateData.PluginState = nil
		privateData.ResultURL = ""
	}
	return data, privateData
}

// TaskBillingContext keeps the accepted immutable quote and separate usage/result evidence.
type TaskBillingContext struct {
	AttemptID       string                `json:"attempt_id"`
	Quote           *billing.Quote        `json:"quote"`
	Usage           *billing.Usage        `json:"usage"`
	Charge          *billing.ChargeResult `json:"charge,omitempty"`
	OriginModelName string                `json:"origin_model_name,omitempty"` // 模型名称，必须为OriginModelName
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// GetResultURL 获取任务结果 URL（视频地址等）
// 新数据存在 PrivateData.ResultURL 中；旧数据回退到 FailReason（历史兼容）
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	*p = TaskPrivateData{}
	switch value := val.(type) {
	case nil:
		return nil
	case []byte:
		if len(value) == 0 {
			return nil
		}
		return common.Unmarshal(value, p)
	case string:
		if value == "" {
			return nil
		}
		return common.UnmarshalJsonStr(value, p)
	default:
		return fmt.Errorf("unsupported task private data database value %T", val)
	}
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	data, err := common.Marshal(p)
	if err == nil && string(data) == "{}" {
		return nil, nil
	}
	return string(data), err
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{CredentialVersion: 1}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		privateData.ChannelKeyIndex = relayInfo.ChannelMeta.ChannelMultiKeyIndex
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   relayInfo.ChannelId,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

// BindCredential runs before submission so later channel edits cannot change
// the owner recorded for an accepted upstream task.
func (p *TaskPrivateData) BindCredential(ctx context.Context, channel *Channel, key string) error {
	if channel == nil || channel.Status != common.ChannelStatusEnabled {
		return ErrGatewayStateUnavailable
	}
	credential, err := EnsureGatewayCredential(ctx, channel.Id, key)
	if err != nil {
		return err
	}
	_, index, err := ResolveGatewayCredential(ctx, channel, credential.ID)
	if err != nil {
		return err
	}
	p.Key = ""
	p.ChannelKeyIndex = index
	p.CredentialVersion = 1
	p.CredentialID = credential.ID
	p.EndpointID = channel.GatewayEndpointID()
	return nil
}

// ResolveTaskCredential keeps pre-binding task records readable while every
// versioned record fails closed if its original credential or endpoint changed.
func ResolveTaskCredential(ctx context.Context, channel *Channel, task *Task) (string, int, error) {
	if task != nil && (task.PrivateData.CredentialVersion != 0 || task.PrivateData.CredentialID != "" || task.PrivateData.EndpointID != "") {
		binding := task.PrivateData
		if channel == nil || task.ChannelId != channel.Id || channel.Status != common.ChannelStatusEnabled ||
			binding.CredentialVersion != 1 || binding.CredentialID == "" || binding.EndpointID != channel.GatewayEndpointID() {
			return "", 0, ErrGatewayStateUnavailable
		}
		key, index, err := ResolveGatewayCredential(ctx, channel, binding.CredentialID)
		return strings.TrimSpace(key), index, err
	}
	if task != nil {
		if legacyKey := strings.TrimSpace(task.PrivateData.Key); legacyKey != "" {
			return legacyKey, task.PrivateData.ChannelKeyIndex, nil
		}
	}
	if channel == nil {
		return "", 0, ErrGatewayStateUnavailable
	}

	keyIndex := 0
	if task != nil {
		keyIndex = task.PrivateData.ChannelKeyIndex
	}
	keys := channel.GetKeys()
	if keyIndex < 0 || keyIndex >= len(keys) {
		return "", 0, ErrGatewayStateUnavailable
	}
	key := strings.TrimSpace(keys[keyIndex])
	if key == "" {
		return "", 0, ErrGatewayStateUnavailable
	}
	return key, keyIndex, nil
}

// ResolveTaskChannelKey is the compatibility entry point for task polling and
// result downloads, where an empty credential already means unavailable.
func ResolveTaskChannelKey(channel *Channel, task *Task) string {
	key, _, _ := ResolveTaskCredential(context.Background(), channel, task)
	return key
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	var tasks []*Task
	err := DB.Where("progress != ?", "100%").
		Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetAllUnFinishSyncTasks(limit int) []*Task {
	var tasks []*Task
	var err error
	// get all tasks progress is not 100%
	err = DB.Where("progress != ?", "100%").Where("status != ?", TaskStatusFailure).Where("status != ?", TaskStatusSuccess).Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedSyncTasks reports whether at least one async (Suno/video) task is
// still in progress. It is a cheap existence check (LIMIT 1) used to decide
// whether the async_task_poll system task needs to run; when no task is pending
// the scheduler skips creating a row entirely.
func HasUnfinishedSyncTasks() bool {
	var id int64
	err := DB.Model(&Task{}).
		Where("progress != ?", "100%").
		Where("status != ?", TaskStatusFailure).
		Where("status != ?", TaskStatusSuccess).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (Task *Task) Insert() error {
	var err error
	err = DB.Create(Task).Error
	return err
}

type taskSnapshot struct {
	Status       TaskStatus
	Progress     string
	StartTime    int64
	FinishTime   int64
	FailReason   string
	ResultURL    string
	Data         json.RawMessage
	PluginState  json.RawMessage
	PollFailures int
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data) && bytes.Equal(s.PluginState, other.PluginState) && s.PollFailures == other.PollFailures
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:       t.Status,
		Progress:     t.Progress,
		StartTime:    t.StartTime,
		FinishTime:   t.FinishTime,
		FailReason:   t.FailReason,
		ResultURL:    t.PrivateData.ResultURL,
		Data:         t.Data,
		PluginState:  t.PrivateData.PluginState,
		PollFailures: t.PrivateData.PollFailures,
	}
}

func (Task *Task) Update() error {
	var err error
	err = DB.Save(Task).Error
	return err
}

func (t *Task) UpdateQuota() error {
	return DB.Model(t).Update("quota", t.Quota).Error
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus. MySQL commonly
// reports changed rows rather than matched rows, so a same-value no-op update
// can also return false even when the status predicate still matched.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	result := DB.Model(t).Where("status = ?", fromStatus).Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// TaskBulkUpdateByID performs an unconditional bulk UPDATE by primary key IDs.
// WARNING: This function has NO CAS (Compare-And-Swap) guard — it will overwrite
// any concurrent status changes. DO NOT use in billing/quota lifecycle flows
// (e.g., timeout, success, failure transitions that trigger refunds or settlements).
// For terminal transitions involving billing, use PersistTaskCompletion instead.
func TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id in (?)", ids).
		Updates(params).Error
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	if t.Status == TaskStatusSuccess {
		openAIVideo.CompletedAt = t.FinishTime
		if openAIVideo.CompletedAt == 0 {
			openAIVideo.CompletedAt = t.UpdatedAt
		}
	}
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}
