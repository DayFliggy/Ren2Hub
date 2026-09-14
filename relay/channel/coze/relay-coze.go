package coze

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func convertCozeChatRequest(c *gin.Context, request dto.GeneralOpenAIRequest) (*CozeChatRequest, error) {
	var messages []CozeEnterMessage
	for _, message := range request.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		var text strings.Builder
		for _, part := range message.ParseContent() {
			if part.Type == dto.ContentTypeText {
				text.WriteString(part.Text)
			}
		}
		if text.Len() == 0 {
			continue
		}
		kind := "question"
		if message.Role == "assistant" {
			kind = "answer"
		}
		messages = append(messages, CozeEnterMessage{Role: message.Role, Type: kind, Content: text.String(), ContentType: "text"})
	}
	if len(messages) == 0 {
		return nil, errors.New("Coze requires at least one text message")
	}
	user := request.User
	if len(user) != 0 {
		var userID string
		if err := common.Unmarshal(user, &userID); err != nil || userID == "" {
			return nil, errors.New("Coze user must be a non-empty string")
		}
	}
	if len(user) == 0 {
		encoded, err := common.Marshal(helper.GetResponseID(c))
		if err != nil {
			return nil, err
		}
		user = encoded
	}
	cozeRequest := &CozeChatRequest{
		BotId:              c.GetString("bot_id"),
		UserId:             user,
		AdditionalMessages: messages,
		Stream:             lo.FromPtrOr(request.Stream, false),
	}
	return cozeRequest, nil
}

func cozeChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	var cozeResponse CozeChatDetailResponse
	if err := common.DecodeJson(resp.Body, &cozeResponse); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if cozeResponse.Code != 0 {
		return nil, types.NewErrorWithStatusCode(errors.New("coze message retrieval failed"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	usage, err := cozeUsage(CozeChatUsage{InputCount: c.GetInt("coze_input_count"), OutputCount: c.GetInt("coze_output_count"), TokenCount: c.GetInt("coze_token_count")})
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	var text strings.Builder
	created := common.GetTimestamp()
	for _, data := range cozeResponse.Data {
		if data.Type != "answer" {
			continue
		}
		if data.ContentType != "" && data.ContentType != "text" {
			return nil, types.NewErrorWithStatusCode(errors.New("unsupported coze answer content"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		var content string
		if err := common.Unmarshal(data.Content, &content); err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		text.WriteString(content)
		if data.CreatedAt > 0 {
			created = data.CreatedAt
		}
	}
	if text.Len() == 0 {
		return nil, types.NewErrorWithStatusCode(errors.New("coze returned no text answer"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	response := dto.OpenAITextResponse{Id: helper.GetResponseID(c), Object: "chat.completion", Created: created, Model: info.UpstreamModelName, Usage: *usage,
		Choices: []dto.OpenAITextResponseChoice{{Index: 0, Message: dto.Message{Role: "assistant", Content: text.String()}, FinishReason: "stop"}}}
	jsonResponse, err := common.Marshal(response)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err := c.Writer.Write(jsonResponse); err != nil {
		return usage, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	info.MarkValidOutput()
	return usage, nil
}

func cozeUsage(raw CozeChatUsage) (*dto.Usage, error) {
	if raw.InputCount < 0 || raw.OutputCount < 0 || raw.TokenCount < 0 || raw.InputCount > math.MaxInt-raw.OutputCount {
		return nil, errors.New("invalid coze usage")
	}
	total := raw.InputCount + raw.OutputCount
	if raw.TokenCount > 0 && raw.TokenCount < total {
		return nil, errors.New("inconsistent coze usage")
	}
	return &dto.Usage{PromptTokens: raw.InputCount, CompletionTokens: raw.OutputCount, TotalTokens: max(total, raw.TokenCount), UsageSource: "provider"}, nil
}

func cozeChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	id := helper.GetResponseID(c)
	var responseText strings.Builder
	var usage *dto.Usage
	completed := false
	sentRole := false
	helper.StreamScannerHandler(c, resp, info, func(data string, result *helper.StreamResult) {
		switch result.EventName() {
		case "conversation.chat.completed":
			var chat CozeChatResponseData
			if err := common.Unmarshal([]byte(data), &chat); err != nil {
				result.Stop(err)
				return
			}
			var err error
			usage, err = cozeUsage(chat.Usage)
			if err != nil {
				result.Stop(err)
				return
			}
			if err := helper.ObjectData(c, helper.GenerateStopResponse(id, common.GetTimestamp(), info.UpstreamModelName, "stop")); err != nil {
				result.Stop(err)
				return
			}
			completed = true
			result.Done()
		case "conversation.message.delta":
			var message CozeChatV3MessageDetail
			if err := common.Unmarshal([]byte(data), &message); err != nil {
				result.Stop(err)
				return
			}
			if message.Type != "" && message.Type != "answer" {
				return
			}
			if message.ContentType != "" && message.ContentType != "text" {
				result.Stop(errors.New("unsupported coze answer content"))
				return
			}
			var content string
			if err := common.Unmarshal(message.Content, &content); err != nil {
				result.Stop(err)
				return
			}
			response := dto.ChatCompletionsStreamResponse{Id: id, Object: "chat.completion.chunk", Created: common.GetTimestamp(), Model: info.UpstreamModelName, Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0}}}
			response.Choices[0].Delta.SetContentString(content)
			if !sentRole {
				response.Choices[0].Delta.Role = "assistant"
				sentRole = true
			}
			if err := helper.ObjectData(c, response); err != nil {
				result.Stop(err)
				return
			}
			if content != "" {
				responseText.WriteString(content)
				result.MarkValidOutput()
			}
		case "error", "conversation.chat.failed", "conversation.chat.canceled", "conversation.chat.requires_action":
			result.Stop(errors.New("coze stream reported a provider failure"))
		default:
			var event map[string]any
			if err := common.Unmarshal([]byte(data), &event); err != nil {
				result.Stop(err)
			}
		}
	})
	if !completed {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, io.ErrUnexpectedEOF)
	}
	if (usage == nil || usage.TotalTokens == 0) && info.HasValidOutput() {
		usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if err := helper.StreamError(info); err != nil {
		return usage, err
	}
	if info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(id, common.GetTimestamp(), info.UpstreamModelName, *usage)); err != nil {
			return usage, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	}
	helper.Done(c)
	return usage, nil
}

func checkIfChatComplete(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (error, bool) {
	requestURL := fmt.Sprintf("%s/v3/chat/retrieve", info.ChannelBaseUrl)

	query := url.Values{"conversation_id": {c.GetString("coze_conversation_id")}, "chat_id": {c.GetString("coze_chat_id")}}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, requestURL+"?"+query.Encode(), nil)
	if err != nil {
		return err, false
	}
	err = a.SetupRequestHeader(c, &req.Header, info)
	if err != nil {
		return err, false
	}

	resp, err := doRequest(req, info)
	if err != nil {
		return err, false
	}
	if resp.StatusCode != http.StatusOK {
		return service.RelayErrorHandler(c.Request.Context(), resp, false), false
	}
	defer service.CloseResponseBodyGracefully(resp)
	var cozeResponse CozeChatResponse
	if err := common.DecodeJson(resp.Body, &cozeResponse); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway), false
	}
	if cozeResponse.Code != 0 || cozeResponse.Data.Id != c.GetString("coze_chat_id") || cozeResponse.Data.ConversationId != c.GetString("coze_conversation_id") {
		return types.NewErrorWithStatusCode(errors.New("coze status response has invalid identity or error code"), types.ErrorCodeBadResponseBody, http.StatusBadGateway), false
	}
	if cozeResponse.Data.Status == "completed" {
		// 在上下文设置 usage
		c.Set("coze_token_count", cozeResponse.Data.Usage.TokenCount)
		c.Set("coze_output_count", cozeResponse.Data.Usage.OutputCount)
		c.Set("coze_input_count", cozeResponse.Data.Usage.InputCount)
		return nil, true
	}
	if cozeResponse.Data.Status == "created" || cozeResponse.Data.Status == "in_progress" {
		return nil, false
	}
	return types.NewErrorWithStatusCode(errors.New("coze chat did not complete successfully"), types.ErrorCodeBadResponseBody, http.StatusBadGateway), false
}

func getChatDetail(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (*http.Response, error) {
	requestURL := fmt.Sprintf("%s/v3/chat/message/list", info.ChannelBaseUrl)

	query := url.Values{"conversation_id": {c.GetString("coze_conversation_id")}, "chat_id": {c.GetString("coze_chat_id")}}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, requestURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	err = a.SetupRequestHeader(c, &req.Header, info)
	if err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}
	resp, err := doRequest(req, info)
	if err != nil {
		return nil, fmt.Errorf("do request failed: %w", err)
	}
	return resp, nil
}

func doRequest(req *http.Request, info *relaycommon.RelayInfo) (*http.Response, error) {
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	relayClient := *client
	relayClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return relayClient.Do(req)
}
