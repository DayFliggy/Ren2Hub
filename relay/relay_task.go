package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/suno"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int
	ClientResponse any
	Credential     model.TaskPrivateData
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，始终使用原凭据身份），
// 已确认的媒体数量属性用于 remix 请求省略该字段时的计价。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var sunoRequest *dto.SunoSubmitReq
	if c.GetString("platform") == string(constant.TaskPlatformSuno) {
		request, err := suno.ParseRequest(c)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		adaptor := &suno.TaskAdaptor{}
		if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
			return taskErr
		}
		sunoRequest = request
		info.OriginTaskID = sunoRequest.TaskID
	}
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		if sunoRequest != nil {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "task_not_exist", http.StatusBadRequest)
		}
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}
	if originTask.PrivateData.TokenId > 0 && originTask.PrivateData.TokenId != info.TokenId {
		return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "task_not_exist", http.StatusBadRequest)
	}
	if sunoRequest != nil {
		if originTask.PrivateData.TokenId != info.TokenId || originTask.PrivateData.TokenId <= 0 ||
			originTask.Platform != constant.TaskPlatformSuno || !strings.EqualFold(originTask.Action, constant.SunoActionMusic) {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "task_not_exist", http.StatusBadRequest)
		}
		if originTask.Status != model.TaskStatusSuccess {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable)
		}
		var songs []dto.SunoSong
		if err := common.Unmarshal(originTask.Data, &songs); err != nil {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable)
		}
		found := false
		for _, song := range songs {
			if song.ID == sunoRequest.ContinueClipId {
				found = true
				break
			}
		}
		if !found {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "task_not_exist", http.StatusBadRequest)
		}
		if originTask.PrivateData.CredentialVersion != 1 || originTask.PrivateData.CredentialID == "" || originTask.PrivateData.EndpointID == "" {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable)
		}
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// Selection still rechecks token/model permission, then pins the task owner.
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		if sunoRequest != nil {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable)
		}
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable)
	}
	info.LockedChannel = ch
	c.Set("gateway_origin_task", originTask)
	info.OriginTaskID = originTask.GetUpstreamTaskID()

	if info.Action == constant.TaskActionRemix {
		snapshot := originTask.PrivateData.BillingContext
		if snapshot == nil || snapshot.Quote == nil {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_billing_evidence_unavailable", http.StatusServiceUnavailable)
		}
		attributes := map[string]interface{}{}
		for _, name := range []string{"duration", "resolution", "quantity", "quality"} {
			if value, exists := snapshot.Quote.Attributes[name]; exists {
				attributes[name] = value
			}
		}
		c.Set("task_billing_origin_attributes", attributes)
	}

	return nil
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)
	selectedChannel, channelErr := model.GetChannelById(info.ChannelId, true)
	if channelErr != nil || selectedChannel.GetBaseURL() != info.ChannelBaseUrl {
		return nil, service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable)
	}
	endpointID := selectedChannel.GatewayEndpointID()

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 2.5 应用渠道的模型映射（与同步任务对齐）
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest)
	}

	// Admission precedes billing; retries reserve any price increase.
	info.PriceData.ChannelRatio = helper.HandleChannelRatio(c, info)
	if info.BeforeUpstream != nil {
		if err := info.BeforeUpstream(); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, service.GatewayAdmissionErrorCode(err), service.GatewayAdmissionHTTPStatus(err))
		}
	}
	info.ForcePreConsume = true
	info.PriceData.QuotaToPreConsume = info.PriceData.Quota
	info.DeferBillingToSend = true

	var credential model.TaskPrivateData
	beforeSend := info.BeforeSend
	info.BeforeSend = func(body io.Reader) error {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		originAttributes, _ := c.Get("task_billing_origin_attributes")
		inherited, _ := originAttributes.(map[string]interface{})
		if err := helper.FreezeTaskBillingAttributes(info, c.GetHeader("Content-Type"), data, inherited); err != nil {
			return err
		}
		if _, err := helper.ModelPriceHelperPerCall(c, info); err != nil {
			return err
		}
		if value, ok := c.Get("route_live_selection"); ok {
			if selection, valid := value.(service.LiveRouteSelection); valid && !selection.AllowsPriceRatio(info.PriceData.ChannelRatio) {
				return service.ErrRoutePriceRatioExceeded
			}
		}
		body = bytes.NewReader(data)
		ch, err := model.GetChannelById(info.ChannelId, true)
		if err != nil {
			return err
		}
		if ch.GatewayEndpointID() != endpointID {
			return model.ErrGatewayStateUnavailable
		}
		boundKey, _, err := service.ValidateGatewayStateOwner(c, ch.Id)
		if err != nil {
			return err
		}
		if boundKey != "" && boundKey != info.ApiKey {
			return model.ErrGatewayStateUnavailable
		}
		if err := credential.BindCredential(c.Request.Context(), ch, info.ApiKey); err != nil {
			return err
		}
		if beforeSend != nil {
			return beforeSend(body)
		}
		if err := service.PrepareBillingForSelectedModel(c, info); err != nil {
			return err
		}
		return nil
	}
	info.UpstreamSideEffecting = true
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		if !info.UpstreamSent {
			var apiErr *types.NewAPIError
			if errors.As(err, &apiErr) {
				return nil, service.TaskErrorFromAPIError(apiErr)
			}
			return nil, service.TaskErrorWrapperLocal(err, "request_preparation_failed", http.StatusServiceUnavailable)
		}
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if resp == nil {
		return nil, service.TaskErrorWrapper(errors.New("empty upstream response"), "invalid_response", http.StatusBadGateway)
	}
	defer resp.Body.Close()
	info.UpstreamAccepted = resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if !info.UpstreamAccepted {
		responseBody, _ := io.ReadAll(resp.Body)
		apiErr := types.NewErrorWithStatusCode(errors.New(string(responseBody)), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
		apiErr.RetryAfter, _ = service.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return nil, service.TaskErrorWrapper(apiErr, "fail_to_fetch_task", resp.StatusCode)
	}

	// 11. 解析响应
	upstreamTaskID, taskData, clientResponse, taskErr := adaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		taskErr.LocalError = false
		if taskErr.StatusCode < 400 || taskErr.StatusCode > 599 {
			taskErr.StatusCode = http.StatusBadGateway
		}
		return nil, taskErr
	}
	if strings.TrimSpace(upstreamTaskID) == "" || clientResponse == nil {
		return nil, service.TaskErrorWrapper(errors.New("accepted task response is incomplete"), "invalid_response", http.StatusBadGateway)
	}

	finalQuota := info.PriceData.Quota

	return &TaskSubmitResult{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		Platform:       platform,
		Quota:          finalQuota,
		ClientResponse: clientResponse,
		Credential:     credential,
	}, nil
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func taskModelName(task *model.Task) string {
	return service.StoredTaskModelName(task)
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			if err := service.TokenModelPermissionError(c, taskModelName(task)); err != nil {
				return nil, service.TaskErrorFromAPIError(err)
			}
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	if err := service.TokenModelPermissionError(c, taskModelName(originTask)); err != nil {
		return nil, service.TaskErrorFromAPIError(err)
	}
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")
	if err := service.TokenModelPermissionError(c, taskModelName(originTask)); err != nil {
		return nil, service.TaskErrorFromAPIError(err)
	}

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}

	key, _, err := model.ResolveTaskCredential(context.Background(), channelModel, task)
	if err != nil {
		return nil
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	ti, err := adaptor.ParseTaskResult(body)
	if err != nil || ti == nil {
		return nil
	}

	// This read may enrich the client response, but only the polling lifecycle
	// persists terminal status together with its billing transition.
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}
