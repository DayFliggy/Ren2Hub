package ali

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func oaiImage2AliImageRequest(info *relaycommon.RelayInfo, request dto.ImageRequest, isSync bool) (*AliImageRequest, error) {
	var imageRequest AliImageRequest
	imageRequest.Model = request.Model
	imageRequest.ResponseFormat = request.ResponseFormat
	count := lo.FromPtrOr(request.N, uint(1))
	if count > dto.MaxImageN {
		return nil, fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
	}
	imageRequest.Parameters = AliImageParameters{
		Size:      strings.ReplaceAll(request.Size, "x", "*"),
		N:         int(count),
		Watermark: request.Watermark,
	}
	if request.Extra != nil {
		if val, ok := request.Extra["parameters"]; ok {
			if strings.TrimSpace(string(val)) == "null" {
				return nil, fmt.Errorf("parameters must be an object")
			}
			imageRequest.Parameters = AliImageParameters{}
			err := common.Unmarshal(val, &imageRequest.Parameters)
			if err != nil {
				return nil, fmt.Errorf("invalid parameters field: %w", err)
			}
		}
		if val, ok := request.Extra["input"]; ok {
			err := common.Unmarshal(val, &imageRequest.Input)
			if err != nil {
				return nil, fmt.Errorf("invalid input field: %w", err)
			}
		}
	}

	if strings.Contains(request.Model, "z-image") {
		// z-image 开启prompt_extend后，按2倍计费
		if imageRequest.Parameters.PromptExtendValue() {
			info.PriceData.AddOtherRatio("prompt_extend", 2)
		}
	}

	// Parameters may come from Extra["parameters"], bypassing the standard
	// top-level n validation; enforce the same bound before it becomes a
	// billing multiplier.
	if imageRequest.Parameters.N < 0 || imageRequest.Parameters.N > dto.MaxImageN {
		return nil, fmt.Errorf("parameters.n must be an integer between 1 and %d", dto.MaxImageN)
	}
	if imageRequest.Parameters.N == 0 {
		imageRequest.Parameters.N = 1
	}
	if imageRequest.Parameters.N != 0 {
		info.PriceData.AddOtherRatio("n", float64(imageRequest.Parameters.N))
	}

	// 同步图片模型和异步图片模型请求格式不一样
	if isSync {
		if imageRequest.Input == nil {
			imageRequest.Input = AliImageInput{
				Messages: []AliMessage{
					{
						Role: "user",
						Content: []AliMediaContent{
							{
								Text: request.Prompt,
							},
						},
					},
				},
			}
		}
	} else {
		if imageRequest.Input == nil {
			imageRequest.Input = AliImageInput{
				Prompt: request.Prompt,
			}
		}
	}

	return &imageRequest, nil
}
func getImageBase64sFromForm(c *gin.Context, fieldName string) ([]string, error) {
	mf := c.Request.MultipartForm
	if mf == nil {
		if _, err := c.MultipartForm(); err != nil {
			return nil, fmt.Errorf("failed to parse image edit form request: %w", err)
		}
		mf = c.Request.MultipartForm
	}

	var imageFiles []*multipart.FileHeader
	var exists bool

	// First check for standard "image" field
	if imageFiles, exists = mf.File["image"]; !exists || len(imageFiles) == 0 {
		// If not found, check for "image[]" field
		if imageFiles, exists = mf.File["image[]"]; !exists || len(imageFiles) == 0 {
			// If still not found, iterate through all fields to find any that start with "image["
			foundArrayImages := false
			for fieldName, files := range mf.File {
				if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
					foundArrayImages = true
					imageFiles = append(imageFiles, files...)
				}
			}

			// If no image fields found at all
			if !foundArrayImages && (len(imageFiles) == 0) {
				return nil, errors.New("image is required")
			}
		}
	}

	if len(imageFiles) == 0 {
		return nil, errors.New("image is required")
	}

	//if len(imageFiles) > 1 {
	//	return nil, errors.New("only one image is supported for qwen edit")
	//}

	// 获取base64编码的图片
	var imageBase64s []string
	for _, file := range imageFiles {
		image, err := file.Open()
		if err != nil {
			return nil, errors.New("failed to open image file")
		}

		// 读取文件内容
		imageData, err := io.ReadAll(image)
		if err != nil {
			return nil, errors.New("failed to read image file")
		}

		// 获取MIME类型
		mimeType := http.DetectContentType(imageData)

		// 编码为base64
		base64Data := base64.StdEncoding.EncodeToString(imageData)

		// 构造data URL格式
		dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
		imageBase64s = append(imageBase64s, dataURL)
		image.Close()
	}
	return imageBase64s, nil
}

func oaiFormEdit2AliImageEdit(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (*AliImageRequest, error) {
	var imageRequest AliImageRequest
	imageRequest.Model = request.Model
	imageRequest.ResponseFormat = request.ResponseFormat

	imageBase64s, err := getImageBase64sFromForm(c, "image")
	if err != nil {
		return nil, fmt.Errorf("get image base64s from form failed: %w", err)
	}
	//dto.MediaContent{}
	mediaContents := make([]AliMediaContent, len(imageBase64s))
	for i, b64 := range imageBase64s {
		mediaContents[i] = AliMediaContent{
			Image: b64,
		}
	}
	mediaContents = append(mediaContents, AliMediaContent{
		Text: request.Prompt,
	})
	imageRequest.Input = AliImageInput{
		Messages: []AliMessage{
			{
				Role:    "user",
				Content: mediaContents,
			},
		},
	}
	imageRequest.Parameters = AliImageParameters{
		N:         int(lo.FromPtrOr(request.N, uint(1))),
		Watermark: request.Watermark,
	}
	return &imageRequest, nil
}

func updateTask(ctx context.Context, info *relaycommon.RelayInfo, taskID string, submitted *http.Request) (*AliResponse, []byte, error) {
	endpoint := fmt.Sprintf("%s/api/v1/tasks/%s", strings.TrimRight(info.ChannelBaseUrl, "/"), url.PathEscape(taskID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header = submitted.Header.Clone()
	req.Host = submitted.Host
	for _, name := range []string{"Content-Length", "Transfer-Encoding", "X-DashScope-Async", "X-DashScope-SSE"} {
		req.Header.Del(name)
	}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return nil, nil, err
	}
	pollClient := *client
	pollClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := pollClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(ctx, resp, false)
		if apiErr.StatusCode < http.StatusBadRequest {
			apiErr.StatusCode = http.StatusBadGateway
		}
		return nil, nil, apiErr
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read Ali task response: %w", err)
	}
	var response AliResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, nil, fmt.Errorf("invalid Ali task response: %w", err)
	}
	if response.Code != "" || response.Message != "" {
		return nil, nil, types.WithOpenAIError(types.OpenAIError{Message: response.Message, Type: "ali_error", Code: response.Code}, http.StatusBadGateway)
	}
	if response.Output.TaskId != "" && response.Output.TaskId != taskID {
		return nil, nil, fmt.Errorf("Ali task response belongs to a different task")
	}
	return &response, responseBody, nil
}

func asyncTaskWait(c *gin.Context, info *relaycommon.RelayInfo, taskID string, submitted *http.Request) (*AliResponse, []byte, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, nil, fmt.Errorf("Ali image response has no task ID")
	}
	if submitted == nil {
		var err error
		submitted, err = channel.BuildApiRequest(&Adaptor{}, c, info, nil)
		if err != nil {
			return nil, nil, err
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Second)
	defer cancel()
	for step := 0; step < 20; step++ {
		rsp, body, err := updateTask(ctx, info, taskID, submitted)
		if err != nil {
			return nil, nil, err
		}
		switch rsp.Output.TaskStatus {
		case "SUCCEEDED":
			return rsp, body, nil
		case "PENDING", "RUNNING":
		case "FAILED", "CANCELED", "UNKNOWN":
			message := rsp.Output.Message
			if message == "" {
				message = "Ali image task " + rsp.Output.TaskStatus
			}
			return nil, nil, types.WithOpenAIError(types.OpenAIError{Message: message, Type: "ali_error", Code: rsp.Output.Code}, http.StatusBadGateway)
		default:
			return nil, nil, fmt.Errorf("invalid Ali task status %q", rsp.Output.TaskStatus)
		}
		if step == 19 {
			break
		}
		timer := time.NewTimer(10 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil, fmt.Errorf("Ali image task polling limit exceeded")
}

func responseAli2OpenAIImage(c *gin.Context, response *AliResponse, originBody []byte, info *relaycommon.RelayInfo, responseFormat string) (*dto.ImageResponse, error) {
	imageResponse := dto.ImageResponse{
		Created: info.StartTime.Unix(),
	}
	var err error
	if len(response.Output.Results) > 0 {
		imageResponse.Data, err = response.Output.ResultToOpenAIImageDate(c, responseFormat)
	} else if len(response.Output.Choices) > 0 {
		imageResponse.Data, err = response.Output.ChoicesToOpenAIImageDate(c, responseFormat)
	}
	if err != nil {
		return nil, err
	}

	imageResponse.Metadata = originBody
	return &imageResponse, nil
}

func aliImageHandler(a *Adaptor, c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.Usage) {
	responseFormat := c.GetString("response_format")
	defer service.CloseResponseBodyGracefully(resp)

	var aliTaskResponse AliResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway), nil
	}
	err = common.Unmarshal(responseBody, &aliTaskResponse)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
	}

	if aliTaskResponse.Code != "" || aliTaskResponse.Message != "" {
		return types.WithOpenAIError(types.OpenAIError{Message: aliTaskResponse.Message, Type: "ali_error", Code: aliTaskResponse.Code}, http.StatusBadGateway), nil
	}

	var (
		aliResponse    *AliResponse
		originRespBody []byte
	)

	if a.IsSyncImageModel {
		aliResponse = &aliTaskResponse
		originRespBody = responseBody
	} else {
		// 异步图片模型需要轮询任务结果
		aliResponse, originRespBody, err = asyncTaskWait(c, info, aliTaskResponse.Output.TaskId, resp.Request)
		if err != nil {
			return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway), nil
		}
	}

	if a.IsSyncImageModel {
		logger.LogDebug(c, "ali_sync_image_result: %s", originRespBody)
	} else {
		logger.LogDebug(c, "ali_async_image_result: %s", originRespBody)
	}

	imageResponses, err := responseAli2OpenAIImage(c, aliResponse, originRespBody, info, responseFormat)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway), nil
	}
	usage, apiErr := openai.DeliverConvertedImage(c, info, resp, imageResponses, nil)
	return apiErr, usage
}
