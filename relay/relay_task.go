package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
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
	Immediate      *relaycommon.TaskInfo
	PluginState    []byte
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，始终使用原凭据身份），
// 已确认的媒体数量属性用于 remix 请求省略该字段时的计价。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info.LockedChannel != nil {
		return nil
	}
	if value, ok := c.Get("task_plugin_origin_tasks"); ok {
		tasks, valid := value.([]*model.Task)
		if !valid || len(tasks) == 0 {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "origin_task_invalid", http.StatusBadRequest, common.UpstreamAddresses(c)...)
		}
		channel, err := model.GetChannelById(tasks[0].ChannelId, true)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "origin_channel_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
		}
		for _, task := range tasks {
			if task.UserId != info.UserId || task.PrivateData.TokenId != info.TokenId || task.ChannelId != channel.Id {
				return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "origin_task_invalid", http.StatusNotFound, common.UpstreamAddresses(c)...)
			}
			if _, _, err := model.ResolveTaskCredential(c.Request.Context(), channel, task); err != nil {
				return service.TaskErrorWrapperLocal(err, "origin_task_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
			}
			info.OriginTasks = append(info.OriginTasks, relaycommon.OriginTaskRef{TaskID: task.TaskID, UpstreamTaskID: task.GetUpstreamTaskID(), Action: task.Action, Status: string(task.Status), Data: task.Data})
		}
		info.LockedChannel = channel
		return nil
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
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest, common.UpstreamAddresses(c)...)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError, common.UpstreamAddresses(c)...)
	}
	if !exist || !originTask.ResultRetrievable() || (c.GetInt("token_id") > 0 && originTask.PrivateData.TokenId != c.GetInt("token_id")) {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest, common.UpstreamAddresses(c)...)
	}
	if originTask.PrivateData.TokenId > 0 && originTask.PrivateData.TokenId != info.TokenId {
		return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnknown, "task_not_exist", http.StatusBadRequest, common.UpstreamAddresses(c)...)
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
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest, common.UpstreamAddresses(c)...)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
	}
	info.LockedChannel = ch
	c.Set("gateway_origin_task", originTask)
	info.OriginTaskID = originTask.GetUpstreamTaskID()

	if info.Action == constant.TaskActionRemix {
		snapshot := originTask.PrivateData.BillingContext
		if snapshot == nil || snapshot.Quote == nil {
			return service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_billing_evidence_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
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

func ApplyOriginTaskAffinity(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return ResolveOriginTask(c, info)
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 最终请求事实 → 冻结报价 → 持久预留 → 发送与保存任务 → 按完成事实结算。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)
	c.Set("pluginAllowedHosts", []string(nil))
	selectedChannel, channelErr := model.GetChannelById(info.ChannelId, true)
	if channelErr != nil || selectedChannel.GetBaseURL() != info.ChannelBaseUrl {
		return nil, service.TaskErrorWrapperLocal(model.ErrGatewayStateUnavailable, "task_state_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
	}
	endpointID := selectedChannel.GatewayEndpointID()

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if _, ok := c.Get(pluginruntime.ContextKeyPinnedPlugin); !ok {
		var plugin *pluginruntime.LoadedPlugin
		var found bool
		plugin, found = pluginruntime.DefaultRegistry.Get(string(platform))
		if !found {
			plugin, found = pluginruntime.DefaultRegistry.GetByChannelType(selectedChannel.Type)
		}
		if found {
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Generation: pluginruntime.DefaultRegistry.Generation(), Plugin: plugin})
		}
	}
	if value, ok := c.Get(pluginruntime.ContextKeyPinnedPlugin); ok {
		if pinned, valid := value.(pluginruntime.PinnedPlugin); valid && pinned.Plugin != nil {
			adaptor = taskplugin.New(pinned.Plugin)
			c.Set("pluginAllowedHosts", append([]string(nil), pinned.Plugin.Meta.AllowedHosts...))
			platform = constant.TaskPlatform(pinned.Plugin.Meta.Key)
			info.TaskPluginRef = &model.TaskPluginRef{Key: pinned.Plugin.Meta.Key, Version: pinned.Plugin.Meta.Version, SourceHash: pinned.Plugin.SourceHash}
			if !service.TaskPluginChannelEligible(selectedChannel, pinned.Plugin.Meta.Key) {
				return nil, service.TaskErrorWrapperLocal(errors.New("channel is not bound to the pinned plugin"), "plugin_channel_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
			}
		}
	}
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest, common.UpstreamAddresses(c)...)
	}
	// Resolve the final upstream identity before a plugin builds its descriptor.
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 2.5 应用渠道的模型映射（与同步任务对齐）
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest, common.UpstreamAddresses(c)...)
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "build_request_failed", http.StatusBadRequest, common.UpstreamAddresses(c)...)
	}

	// Admission precedes billing; retries reserve any price increase.
	helper.HandleChannelRatio(c, info)
	if info.BeforeUpstream != nil {
		if err := info.BeforeUpstream(); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, service.GatewayAdmissionErrorCode(err), service.GatewayAdmissionHTTPStatus(err), common.UpstreamAddresses(c)...)
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
		if provider, ok := adaptor.(channel.TaskValidatedUsageFactsProvider); ok {
			facts, err := provider.ExtractUsageFactsValidated(c, info)
			if err != nil {
				return err
			}
			if _, err := helper.ModelPriceHelperWithTaskFacts(c, info, facts); err != nil {
				return err
			}
		} else if _, err := helper.ModelPriceHelperPerCall(c, info); err != nil {
			return err
		}
		if value, ok := c.Get("route_live_selection"); ok {
			if selection, valid := value.(service.LiveRouteSelection); valid && !selection.AllowsPriceRatio(info.PriceData.ChannelMultiplier()) {
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
		return helper.PinBillingPrice(c, info)
	}
	info.UpstreamSideEffecting = true
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		if !info.UpstreamSent {
			var apiErr *types.NewAPIError
			if errors.As(err, &apiErr) {
				return nil, service.TaskErrorFromAPIError(apiErr, common.UpstreamAddresses(c)...)
			}
			return nil, service.TaskErrorWrapperLocal(err, "request_preparation_failed", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
		}
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError, common.UpstreamAddresses(c)...)
	}
	if resp == nil {
		return nil, service.TaskErrorWrapper(errors.New("empty upstream response"), "invalid_response", http.StatusBadGateway, common.UpstreamAddresses(c)...)
	}
	defer resp.Body.Close()
	info.UpstreamAccepted = resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if !info.UpstreamAccepted {
		responseBody, _ := io.ReadAll(resp.Body)
		apiErr := types.NewErrorWithStatusCode(errors.New(string(responseBody)), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
		apiErr.RetryAfter, _ = service.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return nil, service.TaskErrorWrapper(apiErr, "fail_to_fetch_task", resp.StatusCode, common.UpstreamAddresses(c)...)
	}

	// 11. 解析响应
	var parsed *channel.TaskSubmitResponse
	var taskErr *dto.TaskError
	if plugin, ok := adaptor.(*taskplugin.TaskAdaptor); ok {
		parsed, taskErr = plugin.ParseResponse(c, resp, info)
	} else {
		id, data, response, err := adaptor.DoResponse(c, resp, info)
		parsed = &channel.TaskSubmitResponse{UpstreamTaskID: id, TaskData: data, ClientResponse: response}
		taskErr = err
	}
	if taskErr != nil {
		taskErr.LocalError = false
		if taskErr.StatusCode < 400 || taskErr.StatusCode > 599 {
			taskErr.StatusCode = http.StatusBadGateway
		}
		return nil, taskErr
	}
	if parsed == nil || strings.TrimSpace(parsed.UpstreamTaskID) == "" || (parsed.ClientResponse == nil && info.TaskPluginRef == nil) {
		return nil, service.TaskErrorWrapper(errors.New("accepted task response is incomplete"), "invalid_response", http.StatusBadGateway, common.UpstreamAddresses(c)...)
	}
	info.UpstreamTaskAccepted = true

	finalQuota := info.PriceData.Quota

	return &TaskSubmitResult{
		UpstreamTaskID: parsed.UpstreamTaskID,
		TaskData:       parsed.TaskData,
		Platform:       platform,
		Quota:          finalQuota,
		ClientResponse: parsed.ClientResponse,
		Immediate:      parsed.Immediate,
		PluginState:    parsed.PluginState,
		Credential:     credential,
	}, nil
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest, common.UpstreamAddresses(c)...)
		return
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
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError, common.UpstreamAddresses(c)...)
		return
	}
	return
}

func taskModelName(task *model.Task) string {
	return service.StoredTaskModelName(task)
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError, common.UpstreamAddresses(c)...)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest, common.UpstreamAddresses(c)...)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")
	if !originTask.ResultRetrievable() || (c.GetInt("token_id") > 0 && originTask.PrivateData.TokenId != c.GetInt("token_id")) {
		return nil, service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusNotFound, common.UpstreamAddresses(c)...)
	}
	if err := service.TokenModelPermissionError(c, taskModelName(originTask)); err != nil {
		return nil, service.TaskErrorFromAPIError(err, common.UpstreamAddresses(c)...)
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if originTask.PrivateData.PluginRef != nil {
			var resolveErr error
			adaptor, resolveErr = GetTaskAdaptorForTask(originTask)
			if resolveErr != nil {
				return nil, service.TaskErrorWrapperLocal(resolveErr, "plugin_unavailable", http.StatusServiceUnavailable, common.UpstreamAddresses(c)...)
			}
		}
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest, common.UpstreamAddresses(c)...)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError, common.UpstreamAddresses(c)...)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented, common.UpstreamAddresses(c)...)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError, common.UpstreamAddresses(c)...)
	}
	return
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
