package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if action := c.GetString("task_action"); action != "" {
		relayInfo.Action = action
	}
	if name := c.GetString("resolved_task_model"); name != "" {
		relayInfo.OriginModelName = name
	}
	outcome, taskErr := executeTaskSubmission(c, relayInfo)
	if taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}
	presentTaskSubmission(c, outcome)
}

type taskSubmissionOutcome struct {
	Result    *relay.TaskSubmitResult
	Task      *model.Task
	RelayInfo *relaycommon.RelayInfo
}

func RelayTaskPluginEndpoint(c *gin.Context, fallback gin.HandlerFunc) {
	value, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	if !exists {
		fallback(c)
		return
	}
	pinned, ok := value.(pluginruntime.PinnedEndpoint)
	if !ok || pinned.Plugin == nil || pinned.Generation == nil {
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
	switch pinned.Protocol {
	case "openai_responses":
		serveTaskPluginProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
	case pluginruntime.ProtocolOpenAIImage:
		serveTaskPluginImageProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
	default:
		fallback(c)
	}
}

func taskSubmissionAPIError(err *taskdto.TaskError) *types.NewAPIError {
	return service.APIErrorFromTaskError(err)
}

func executeTaskSubmission(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*taskSubmissionOutcome, *taskdto.TaskError) {

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		return nil, taskErr
	}

	if locked, ok := relayInfo.LockedChannel.(*model.Channel); ok && locked != nil {
		if _, routeErr := middleware.SelectRequestChannel(c, relayInfo.OriginModelName, locked.Id); routeErr != nil {
			return nil, service.TaskErrorFromAPIError(routeErr)
		}
		relayInfo.InitChannelMeta(c)
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	durable := false
	defer func() {
		releaseRouteAttempt(c)
		finalizeLiveRouteDecision(c, relayInfo, service.APIErrorFromTaskError(taskErr))
		if !durable && taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:   c,
		Retry: common.GetPointer(0),
	}

	for {
		if (routeLiveSelectionActive(c) && liveRouteRetryCounters(c).TotalAttempts >= service.DefaultTotalAttempts) ||
			(!routeLiveSelectionActive(c) && retryParam.GetRetry() > relayRetryLimit(c)) {
			break
		}
		if _, exists := c.Get("route_channel_permit"); exists {
			releaseRouteAttempt(c)
			if routeLiveSelectionActive(c) && c.GetBool(liveRouteRenewalFailedKey) {
				break
			}
		}
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			if taskErr == nil || !errors.Is(channelErr, service.ErrRouteSelectionUnavailable) {
				taskErr = service.TaskErrorWrapperLocal(channelErr, "get_channel_failed", http.StatusServiceUnavailable)
			}
			break
		}
		if resetErr := relayInfo.ResetAttemptState(); resetErr != nil {
			taskErr = service.TaskErrorWrapperLocal(resetErr, "attempt_reset_failed", http.StatusInternalServerError)
			break
		}
		var admissionErr error
		relayInfo.BeforeUpstream = func() error {
			admissionErr = admitMediaRouteAttempt(c, relayInfo, retryParam.GetRetry())
			return admissionErr
		}
		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		attemptStartedAt := time.Now()
		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		attemptError := service.APIErrorFromTaskError(taskErr)
		outcome := relayAttemptOutcome(c, relayInfo, attemptError, attemptStartedAt, true)
		observeLiveRouteAttempt(c, relayInfo, outcome)
		if admissionErr != nil {
			if markLiveRouteAdmissionRejection(c, channel.Id, admissionErr) {
				if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
			}
			break
		}
		if taskErr == nil {
			break
		}
		if taskErr.NoRetry {
			break
		}
		if markLiveRoutePreSendRejection(c, relayInfo, attemptError) {
			if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			break
		}

		if routeLiveSelectionActive(c) {
			if taskErr.Code == "route_price_ratio_exceeded" {
				markLiveRouteCandidateFiltered(c, channel.Id, service.RouteFilterPriceForbidden)
				if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
				break
			}
			classification := service.ClassifyAttemptOutcome(outcome)
			if !taskErr.LocalError && service.CanRouteFailover(classification, relayResponseCommitted(c, relayInfo), false) {
				if nextAttempt, found := nextLiveRouteAttemptForError(c, retryParam.GetRetry(), channel.Id, classification); found {
					if !waitForLiveRouteBackoff(c, retryParam.GetRetry(), liveRouteScopedRetryAfter(c, nextAttempt, channel.Id, outcome.RetryAfter)) {
						break
					}
					retryParam.SetRetry(nextAttempt)
					continue
				}
			}
		}

		if !taskErr.LocalError {
			channelError := types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
				common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			if routeLiveSelectionActive(c) {
				channelError.AutoBan = false
			}
			processChannelError(c,
				*channelError,
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode), relayInfo)
		}

		if routeLiveSelectionActive(c) {
			break
		}
		break
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	if taskErr == nil {
		if result == nil {
			return nil, service.TaskErrorWrapperLocal(errors.New("task submission ended without a result"), "task_unavailable", http.StatusServiceUnavailable)
		}
		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData = result.Credential
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.PluginRef = relayInfo.TaskPluginRef
		task.PrivateData.PluginAttemptID = relayInfo.BillingAttemptID
		task.PrivateData.PluginState = result.PluginState
		task.PrivateData.Execution = service.TaskExecutionSnapshotFromContext(c)
		if value, ok := c.Get(pluginruntime.ContextKeyProtocolRequest); ok {
			if request, valid := value.(pluginruntime.ProtocolRequestContext); valid {
				if body, ok := request.Body.(map[string]any); ok {
					if requestBody, ok := body["value"].(map[string]any); ok {
						task.PrivateData.ResponsesBackground, _ = requestBody["background"].(bool)
					}
				}
			}
		}
		if value, ok := c.Get(pluginruntime.ContextKeyPinnedRoute); ok {
			if pinned, valid := value.(pluginruntime.PinnedRoute); valid && pinned.Route.RetainResult != nil && !*pinned.Route.RetainResult {
				if result.Immediate != nil {
					task.PrivateData.ResultDiscarded = true
				} else {
					logger.LogWarn(c, "native retainResult:false ignored for an accepted asynchronous task")
				}
			}
		}
		if result.Immediate != nil {
			if value, ok := c.Get(pluginruntime.ContextKeyPinnedEndpoint); ok {
				if pinned, valid := value.(pluginruntime.PinnedEndpoint); valid && pinned.Protocol == pluginruntime.ProtocolOpenAIImage {
					task.PrivateData.ResultDiscarded = true
				}
			}
		}
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			Quote: relayInfo.PriceData.Quote, Usage: relayInfo.PriceData.Usage,
			OriginModelName: relayInfo.OriginModelName,
		}
		task.Quota = common.Quota(result.Quota)
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		var persistErr error
		if result.Immediate != nil {
			completionCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
			durable, persistErr = service.FinalizeImmediateTaskSubmission(completionCtx, c, relayInfo, task, result.Immediate)
			cancel()
		} else {
			persistErr = service.FinalizeTaskSubmission(c, relayInfo, task)
			durable = persistErr == nil
		}
		if persistErr != nil {
			common.SysError("persist accepted task error: " + persistErr.Error())
			if durable {
				taskErr = service.TaskErrorWrapperLocal(errors.New("accepted task is saved; accounting recovery is pending; do not resubmit automatically"), "task_completion_pending", http.StatusServiceUnavailable)
			} else {
				taskErr = service.TaskErrorWrapperLocal(errors.New("accepted task could not be saved; do not resubmit automatically"), "task_persistence_failed", http.StatusServiceUnavailable)
			}
		} else {
			return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: relayInfo}, nil
		}
	}

	return nil, taskErr
}

func presentTaskSubmission(c *gin.Context, outcome *taskSubmissionOutcome) {
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedRoute); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedRoute); ok && pinned.Plugin != nil && pinned.Route.Render != "" {
			view, err := service.BuildTaskPluginView(outcome.Task)
			requestValue, _ := c.Get(pluginruntime.ContextKeyRouteRequest)
			request, _ := requestValue.(pluginruntime.RouteRequestContext)
			if err == nil {
				var value any
				value, err = taskPluginProtocolJSONValue(view)
				if err == nil {
					var body any
					body, err = pinned.Plugin.Engine.CallPath(c.Request.Context(), "native", []string{pinned.Route.Render}, request.JSValue(), value)
					if err == nil {
						outcome.RelayInfo.MarkValidOutput()
						c.JSON(http.StatusOK, body)
						return
					}
				}
			}
			respondTaskError(c, service.TaskErrorWrapperLocal(err, "task_presentation_failed", http.StatusBadGateway))
			return
		}
	}
	outcome.RelayInfo.MarkValidOutput()
	if outcome.Result.ClientResponse != nil {
		c.JSON(http.StatusOK, outcome.Result.ClientResponse)
		return
	}
	view, err := service.BuildTaskPluginView(outcome.Task)
	if err != nil {
		respondTaskError(c, service.TaskErrorWrapperLocal(err, "task_presentation_failed", http.StatusBadGateway))
		return
	}
	c.JSON(http.StatusOK, view)
}

func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if requestContextDone(c) || c.Writer.Written() {
		return
	}
	c.JSON(taskErr.StatusCode, taskErr)
}
