package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func RelayMidjourneyImage(c *gin.Context) {
	taskId := c.Param("id")
	expiresAt, err := strconv.ParseInt(strings.TrimSpace(c.Query("exp")), 10, 64)
	valid := service.ValidateMidjourneyImageURL(taskId, expiresAt, c.Query("sig"), time.Now())
	if c.Query("v") == "2" {
		valid = service.ValidateMidjourneyTaskImageURL(taskId, expiresAt, c.Query("sig"), time.Now())
	} else if c.Query("v") != "" {
		valid = false
	}
	if err != nil || !valid {
		c.JSON(http.StatusForbidden, gin.H{"error": "midjourney_image_access_denied"})
		return
	}
	var midjourneyTask *model.Midjourney
	if c.Query("v") == "2" {
		if id, err := strconv.Atoi(taskId); err == nil && id > 0 {
			midjourneyTask = model.GetMjByuId(id)
		}
	} else {
		midjourneyTask = model.GetByOnlyMJId(taskId)
	}
	if midjourneyTask == nil {
		c.JSON(400, gin.H{
			"error": "midjourney_task_not_found",
		})
		return
	}
	var httpClient *http.Client
	var proxy string
	if channel, err := model.CacheGetChannel(midjourneyTask.ChannelId); err == nil {
		proxy = channel.GetSetting().Proxy
		if proxy != "" {
			if httpClient, err = service.GetHttpClientWithProxy(proxy); err != nil {
				c.JSON(400, gin.H{
					"error": "proxy_url_invalid",
				})
				return
			}
		}
	}
	if httpClient == nil {
		httpClient = service.GetSSRFProtectedHTTPClient()
	}
	var validateErr error
	if proxy == "" {
		validateErr = service.ValidateSSRFProtectedFetchURL(midjourneyTask.ImageUrl)
	} else {
		// 渠道代理路径的连接由代理侧建立，无法做拨号时逐 IP 校验，
		// 因此保留请求前的一次性 SSRF 校验。
		fetchSetting := system_setting.GetFetchSetting()
		validateErr = common.ValidateURLWithFetchSetting(midjourneyTask.ImageUrl, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain)
	}
	if validateErr != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf("request blocked: %v", validateErr),
		})
		return
	}
	resp, err := httpClient.Get(midjourneyTask.ImageUrl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "http_get_image_failed",
		})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		c.JSON(resp.StatusCode, gin.H{
			"error": string(responseBody),
		})
		return
	}
	// 从Content-Type头获取MIME类型
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// 如果无法确定内容类型，则默认为jpeg
		contentType = "image/jpeg"
	}
	// 设置响应的内容类型
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.Header().Set("Cache-Control", "private, max-age=300")
	// 将图片流式传输到响应体
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Println("Failed to stream image:", err)
	}
	return
}

func coverMidjourneyTaskDto(c *gin.Context, originTask *model.Midjourney) (midjourneyTask dto.MidjourneyDto) {
	midjourneyTask.MjId = originTask.MjId
	midjourneyTask.Progress = originTask.Progress
	midjourneyTask.PromptEn = originTask.PromptEn
	midjourneyTask.State = originTask.State
	midjourneyTask.SubmitTime = originTask.SubmitTime
	midjourneyTask.StartTime = originTask.StartTime
	midjourneyTask.FinishTime = originTask.FinishTime
	midjourneyTask.ImageUrl = ""
	if originTask.ImageUrl != "" && setting.MjForwardUrlEnabled {
		midjourneyTask.ImageUrl = service.BuildMidjourneyTaskImageURL(system_setting.ServerAddress, originTask.Id)
		if originTask.Status != "SUCCESS" {
			midjourneyTask.ImageUrl += "&rand=" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	} else {
		midjourneyTask.ImageUrl = originTask.ImageUrl
	}
	if originTask.VideoUrl != "" {
		midjourneyTask.VideoUrl = originTask.VideoUrl
	}
	midjourneyTask.Status = originTask.Status
	midjourneyTask.FailReason = originTask.FailReason
	midjourneyTask.Action = originTask.Action
	midjourneyTask.Description = originTask.Description
	midjourneyTask.Prompt = originTask.Prompt
	if originTask.Buttons != "" {
		var buttons []dto.ActionButton
		err := common.Unmarshal([]byte(originTask.Buttons), &buttons)
		if err == nil {
			midjourneyTask.Buttons = buttons
		}
	}
	if originTask.VideoUrls != "" {
		var videoUrls []dto.ImgUrls
		err := common.Unmarshal([]byte(originTask.VideoUrls), &videoUrls)
		if err == nil {
			midjourneyTask.VideoUrls = videoUrls
		}
	}
	if originTask.Properties != "" {
		var properties dto.Properties
		err := common.Unmarshal([]byte(originTask.Properties), &properties)
		if err == nil {
			midjourneyTask.Properties = &properties
		}
	}
	return
}

func RelaySwapFace(c *gin.Context, info *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	var request dto.SwapFaceRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}
	if request.SourceBase64 == "" || request.TargetBase64 == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "source_and_target_are_required")
	}
	info.InitChannelMeta(c)
	return submitMidjourneyJob(c, info, constant.MjActionSwapFace, "InsightFace", true)
}

func RelayMidjourneyTaskImageSeed(c *gin.Context, info *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	task, err := model.GetOwnedMidjourneyTask(c.Request.Context(), info.UserId, info.TokenId, c.Param("id"))
	if err != nil {
		return service.MidjourneyRequestError(info, model.ErrGatewayStateUnknown, types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	}
	info.OriginModelName = service.CovertMjpActionToModelName(task.Action)
	if err := service.TokenModelPermissionError(c, info.OriginModelName); err != nil {
		return service.MidjourneyRequestError(info, err, types.ErrorCodeAccessDenied, http.StatusForbidden)
	}
	c.Set("gateway_origin_midjourney", task)
	if _, routeErr := middleware.SelectRequestChannel(c, info.OriginModelName, task.ChannelId); routeErr != nil {
		return service.MidjourneyRequestError(info, routeErr, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	info.InitChannelMeta(c)
	price, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return service.MidjourneyRequestError(info, err, types.ErrorCodeModelPriceError, http.StatusBadRequest)
	}
	info.PriceData = price
	info.PriceData.Quota, info.PriceData.QuotaToPreConsume = 0, 0
	var credential model.TaskPrivateData
	response, body, err := service.DoMidjourneyHttpRequest(c, info, 30*time.Second, strings.TrimRight(info.ChannelBaseUrl, "/")+getMjRequestPath(c.Request.URL.Path), &credential)
	if err != nil {
		return &response.Response
	}
	c.Data(response.StatusCode, "application/json", body)
	return nil
}

func RelayMidjourneyTask(c *gin.Context, relayMode int) *dto.MidjourneyResponse {
	userId := c.GetInt("id")
	var err error
	var respBody []byte
	switch relayMode {
	case relayconstant.RelayModeMidjourneyTaskFetch:
		taskId := c.Param("id")
		originTask, lookupErr := model.GetOwnedMidjourneyTask(c.Request.Context(), userId, c.GetInt("token_id"), taskId)
		if lookupErr != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "task_no_found",
			}
		}
		if err := service.TokenModelPermissionError(c, service.CovertMjpActionToModelName(originTask.Action)); err != nil {
			return &dto.MidjourneyResponse{Code: 4, Description: "token_model_forbidden"}
		}
		midjourneyTask := coverMidjourneyTaskDto(c, originTask)
		respBody, err = common.Marshal(midjourneyTask)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	case relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		var condition = struct {
			IDs []string `json:"ids"`
		}{}
		err = common.UnmarshalBodyReusable(c, &condition)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "do_request_failed",
			}
		}
		var tasks []dto.MidjourneyDto
		if len(condition.IDs) > 100 {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "too_many_task_ids")
		}
		if len(condition.IDs) != 0 {
			for _, id := range condition.IDs {
				originTask, lookupErr := model.GetOwnedMidjourneyTask(c.Request.Context(), userId, c.GetInt("token_id"), id)
				if lookupErr != nil {
					return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_not_found")
				}
				if err := service.TokenModelPermissionError(c, service.CovertMjpActionToModelName(originTask.Action)); err != nil {
					return &dto.MidjourneyResponse{Code: 4, Description: "token_model_forbidden"}
				}
				midjourneyTask := coverMidjourneyTaskDto(c, originTask)
				tasks = append(tasks, midjourneyTask)
			}
		}
		if tasks == nil {
			tasks = make([]dto.MidjourneyDto, 0)
		}
		respBody, err = common.Marshal(tasks)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")

	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	return nil
}

func RelayMidjourneySubmit(c *gin.Context, relayInfo *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	consumeQuota := true
	var midjRequest dto.MidjourneyRequest
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	relayInfo.InitChannelMeta(c)

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyAction { // midjourney plus，需要从customId中获取任务信息
		mjErr := service.CoverPlusActionToNormalAction(&midjRequest)
		if mjErr != nil {
			return mjErr
		}
		relayInfo.RelayMode = relayconstant.RelayModeMidjourneyChange
	}
	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneySimpleChange {
		params := service.ConvertSimpleChangeParams(midjRequest.Content)
		if params == nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
		}
		midjRequest.TaskId, midjRequest.Action, midjRequest.Index = params.TaskId, params.Action, params.Index
	}
	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
		midjRequest.Action = constant.MjActionVideo
	}

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyImagine { //绘画任务，此类任务可重复
		if midjRequest.Prompt == "" {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "prompt_is_required")
		}
		midjRequest.Action = constant.MjActionImagine
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyDescribe { //按图生文任务，此类任务可重复
		midjRequest.Action = constant.MjActionDescribe
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyEdits { //编辑任务，此类任务可重复
		midjRequest.Action = constant.MjActionEdits
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyShorten { //缩短任务，此类任务可重复，plus only
		midjRequest.Action = constant.MjActionShorten
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyBlend { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionBlend
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyUpload { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionUpload
	} else if midjRequest.TaskId != "" || relayInfo.RelayMode == relayconstant.RelayModeMidjourneyChange || relayInfo.RelayMode == relayconstant.RelayModeMidjourneyModal { //放大、变换任务，此类任务，如果重复且已有结果，远端api会直接返回最终结果
		mjId := ""
		if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyChange {
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			} else if midjRequest.Index == 0 {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "index_is_required")
			}
			//action = midjRequest.Action
			mjId = midjRequest.TaskId
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneySimpleChange {
			if midjRequest.Content == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_is_required")
			}
			params := service.ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
			}
			mjId = params.TaskId
			midjRequest.Action = params.Action
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyModal {
			//if midjRequest.MaskBase64 == "" {
			//	return service.MidjourneyErrorWrapper(constant.MjRequestError, "mask_base64_is_required")
			//}
			mjId = midjRequest.TaskId
			midjRequest.Action = constant.MjActionModal
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
			midjRequest.Action = constant.MjActionVideo
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			}
			mjId = midjRequest.TaskId
		}

		originTask, lookupErr := model.GetOwnedMidjourneyTask(c.Request.Context(), relayInfo.UserId, relayInfo.TokenId, mjId)
		if lookupErr != nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_not_found")
		} else { //原任务的Status=SUCCESS，则可以做放大UPSCALE、变换VARIATION等动作，此时必须使用原来的请求地址才能正确处理
			if setting.MjActionCheckSuccessEnabled {
				if originTask.Status != "SUCCESS" && relayInfo.RelayMode != relayconstant.RelayModeMidjourneyModal {
					return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_status_not_success")
				}
			}
			channel, err := model.GetChannelById(originTask.ChannelId, true)
			if err != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
			}
			if channel.Status != common.ChannelStatusEnabled {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
			}
			if err := service.TokenModelPermissionError(c, service.CovertMjpActionToModelName(originTask.Action)); err != nil {
				return service.MidjourneyRequestError(relayInfo, err, types.ErrorCodeAccessDenied, http.StatusForbidden)
			}
			c.Set("gateway_origin_midjourney", originTask)
			if _, routeErr := middleware.SelectRequestChannel(c, service.CovertMjpActionToModelName(midjRequest.Action), originTask.ChannelId); routeErr != nil {
				return service.MidjourneyRequestError(relayInfo, routeErr, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
			}
			relayInfo.InitChannelMeta(c)
		}
		midjRequest.Prompt = originTask.Prompt
	}

	if midjRequest.Action == constant.MjActionInPaint || midjRequest.Action == constant.MjActionCustomZoom {
		consumeQuota = false
	}

	return submitMidjourneyJob(c, relayInfo, midjRequest.Action, midjRequest.Prompt, consumeQuota)
}

// submitMidjourneyJob persists ownership before exposing the accepted ID. The
// logical request owns both submission billing and any later recovery.
func submitMidjourneyJob(c *gin.Context, info *relaycommon.RelayInfo, action, prompt string, bill bool) *dto.MidjourneyResponse {
	info.Action = action
	info.OriginModelName = service.CovertMjpActionToModelName(action)
	price, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return service.MidjourneyRequestError(info, err, types.ErrorCodeModelPriceError, http.StatusBadRequest)
	}
	info.PriceData = price
	if !bill {
		info.PriceData.Quota, info.PriceData.QuotaToPreConsume = 0, 0
	}
	info.ForcePreConsume, info.DeferBillingToSend, info.UpstreamSideEffecting = true, true, true
	var credential model.TaskPrivateData
	response, body, err := service.DoMidjourneyHttpRequest(c, info, 60*time.Second,
		strings.TrimRight(info.ChannelBaseUrl, "/")+getMjRequestPath(c.Request.URL.Path), &credential)
	if err != nil {
		return &response.Response
	}
	info.UpstreamTaskAccepted = true
	result := response.Response
	now := time.Now().UnixMilli()
	task := &model.Midjourney{
		UserId: info.UserId, ChannelId: info.ChannelId, Code: result.Code,
		Action: action, MjId: result.Result, Prompt: prompt, Description: result.Description,
		SubmitTime: now, Status: "SUBMITTED", Progress: "0%", Quota: common.Quota(info.PriceData.Quota),
		PrivateData: credential,
	}
	immediate := info.RelayMode == relayconstant.RelayModeMidjourneyUpload
	if result.Code == 21 {
		if props, ok := result.Properties.(map[string]any); ok && props["status"] == "SUCCESS" {
			task.ImageUrl, _ = props["imageUrl"].(string)
			immediate = true
		}
	}
	if err := service.FinalizeMidjourneySubmission(c, info, task); err != nil {
		return service.MidjourneyRequestError(info, errors.New("accepted Midjourney task could not be saved; do not resubmit automatically"), types.ErrorCodeUpdateDataError, http.StatusServiceUnavailable)
	}
	// An already finished job completes through the same terminal accounting
	// as polling; polling never revisits a task stored as SUCCESS.
	if immediate {
		from := task.Status
		task.Status, task.Progress, task.FinishTime = "SUCCESS", "100%", now
		completionCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
		committed, err := service.CompleteMidjourneyBilling(completionCtx, task, from)
		cancel()
		if !committed {
			// A result must not escape before its terminal accounting is durable.
			return service.MidjourneyRequestError(info, errors.New("accepted Midjourney result could not be committed; do not resubmit automatically"), types.ErrorCodeUpdateDataError, http.StatusServiceUnavailable)
		}
		if err != nil {
			logger.LogError(c, fmt.Sprintf("Midjourney immediate completion remains pending (row_id=%d): %v", task.Id, err))
		}
	}
	// Preserve extensions and array upload results when normalizing proxy codes.
	if result.Code == 22 || result.Code == 21 && bill {
		var envelope map[string]json.RawMessage
		if err := common.Unmarshal(body, &envelope); err != nil {
			return service.MidjourneyRequestError(info, err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		envelope["code"] = json.RawMessage("1")
		body, err = common.Marshal(envelope)
		if err != nil {
			return service.MidjourneyRequestError(info, err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
		}
	}
	info.MarkValidOutput()
	c.Data(response.StatusCode, "application/json", body)
	return nil
}

func getMjRequestPath(path string) string {
	if index := strings.Index(path, "/mj/"); index >= 0 {
		return path[index:]
	}
	return path
}
