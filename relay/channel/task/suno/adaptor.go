package suno

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
}

// ParseTaskResult is not used for Suno tasks.
// Suno polling uses a dedicated batch-fetch path (service.UpdateSunoTasks) that
// receives dto.TaskResponse[[]dto.SunoDataResponse] from the upstream /fetch API.
// This differs from the per-task polling used by video adaptors.
func (a *TaskAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, fmt.Errorf("suno uses batch polling via UpdateSunoTasks, ParseTaskResult is not applicable")
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	action := strings.ToUpper(c.Param("action"))

	sunoRequest, err := ParseRequest(c)
	if err != nil {
		taskErr = service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		return
	}
	err = actionValidate(c, sunoRequest, action)
	if err != nil {
		taskErr = service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		return
	}

	info.Action = action
	return nil
}

// ParseRequest keeps one validated DTO for pre-routing ownership checks and
// upstream submission. The original public task ID remains unchanged here.
func ParseRequest(c *gin.Context) (*dto.SunoSubmitReq, error) {
	if value, exists := c.Get("task_request"); exists {
		if request, ok := value.(*dto.SunoSubmitReq); ok && request != nil {
			return request, nil
		}
	}
	var request *dto.SunoSubmitReq
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, fmt.Errorf("request body must be an object")
	}
	if request.TaskID != "" || request.ContinueClipId != "" {
		if strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.ContinueClipId) == "" {
			return nil, fmt.Errorf("task_id and continue_clip_id are required together")
		}
		if !strings.EqualFold(c.Param("action"), constant.SunoActionMusic) {
			return nil, fmt.Errorf("continuation requires the music action")
		}
	}
	c.Set("task_request", request)
	return request, nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := info.ChannelBaseUrl
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, "/suno/submit/"+info.Action)
	return fullRequestURL, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	sunoRequest, err := ParseRequest(c)
	if err != nil {
		return nil, err
	}
	request := *sunoRequest
	if request.ContinueClipId != "" {
		if info.OriginTaskID == "" || info.LockedChannel == nil {
			return nil, fmt.Errorf("continuation task ownership was not resolved")
		}
		request.TaskID = info.OriginTaskID
	}
	data, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, clientResponse any, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	var sunoResponse dto.TaskResponse[string]
	err = common.Unmarshal(responseBody, &sunoResponse)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if !sunoResponse.IsSuccess() {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s", sunoResponse.Message), sunoResponse.Code, http.StatusInternalServerError)
		return
	}

	// 使用公开 task_xxxx ID 替换上游 ID 返回给客户端
	publicResponse := dto.TaskResponse[string]{
		Code:    sunoResponse.Code,
		Message: sunoResponse.Message,
		Data:    info.PublicTaskID,
	}
	return sunoResponse.Data, responseBody, publicResponse, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	requestUrl := fmt.Sprintf("%s/suno/fetch", baseUrl)
	byteBody, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", requestUrl, bytes.NewBuffer(byteBody))
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task error: %v", err))
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func actionValidate(c *gin.Context, sunoRequest *dto.SunoSubmitReq, action string) (err error) {
	switch action {
	case constant.SunoActionMusic:
		if sunoRequest.Mv == "" {
			sunoRequest.Mv = "chirp-v3-0"
		}
	case constant.SunoActionLyrics:
		if sunoRequest.Prompt == "" {
			err = fmt.Errorf("prompt_empty")
			return
		}
	default:
		err = fmt.Errorf("invalid_action")
	}
	return
}
