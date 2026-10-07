package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/gin-gonic/gin"
)

func sendStreamData(c *gin.Context, info *relaycommon.RelayInfo, data string, forceFormat bool, thinkToContent bool) error {
	if data == "" {
		return nil
	}
	if !info.ShouldIncludeUsage && gjson.Get(data, "usage").Exists() {
		// Usage may share a frame with text, tool arguments, or finish_reason.
		if len(gjson.Get(data, "choices").Array()) == 0 {
			return nil
		}
		var err error
		data, err = sjson.Delete(data, "usage")
		if err != nil {
			return err
		}
	}

	if !forceFormat && !thinkToContent {
		return helper.StringData(c, data)
	}

	var lastStreamResponse dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &lastStreamResponse); err != nil {
		return err
	}

	if !thinkToContent {
		return helper.ObjectData(c, lastStreamResponse)
	}

	convertThinkingToContent(&lastStreamResponse, &info.ThinkingContentInfo)
	return helper.ObjectData(c, lastStreamResponse)
}

func convertThinkingToContent(response *dto.ChatCompletionsStreamResponse, state *relaycommon.ThinkingContentInfo) {
	if state.OpenThinkingChoices == nil {
		state.OpenThinkingChoices = make(map[int]bool)
	}
	for i := range response.Choices {
		choice := &response.Choices[i]
		delta := &choice.Delta
		var content strings.Builder
		open := state.OpenThinkingChoices[choice.Index]
		if reasoning := delta.GetReasoningContent(); reasoning != "" {
			if !open {
				content.WriteString("<think>\n")
				open = true
			}
			content.WriteString(reasoning)
			state.IsFirstThinkingContent, state.HasSentThinkingContent = false, true
		}
		finished := choice.FinishReason != nil && *choice.FinishReason != ""
		if open && (delta.GetContentString() != "" || len(delta.ToolCalls) != 0 || finished) {
			content.WriteString("\n</think>\n")
			open = false
			state.SendLastThinkingContent = true
		}
		// Keep tags and text in the original delta so tools and finish_reason
		// are emitted exactly once, including when reasoning shares that frame.
		if content.Len() != 0 {
			content.WriteString(delta.GetContentString())
			delta.SetContentString(content.String())
		}
		delta.ReasoningContent, delta.Reasoning = nil, nil
		if open {
			state.OpenThinkingChoices[choice.Index] = true
		} else {
			delete(state.OpenThinkingChoices, choice.Index)
		}
	}
}

func OaiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	c.Set("openai_response_stream_state", nil)
	model := info.UpstreamModelName
	var responseId string
	var createAt int64 = 0
	var systemFingerprint string
	var containStreamUsage bool
	var responseTextBuilder strings.Builder
	var toolCount int
	var usage = &dto.Usage{}
	var lastStreamData string
	var usageStreamData string
	tail := chatStreamTail{response: dto.ChatCompletionsStreamResponse{Model: model}}
	seenStreamToolCalls := make(map[string]struct{})
	var streamFunctionCallNames []string

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var observed dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &observed); err != nil {
			sr.Stop(err)
			return
		}
		if err := service.NormalizeProviderUsage(observed.Usage); err != nil {
			sr.Stop(err)
			return
		}
		info.ObserveResponseModel(observed.Model)
		info.ObserveActualServiceTier(observed.ServiceTier)
		lastStreamData = data
		if terminal := relayconvert.TerminationFromResponse(&observed); terminal.IsTerminal() {
			c.Set("response_termination", terminal)
		}
		tail.observe(observed)
		if observed.Usage != nil {
			if !dto.HasJSONTokenCounts([]byte(gjson.Get(data, "usage").Raw), "prompt_tokens", "completion_tokens") {
				observed.Usage.UsageSource = "partial"
				observed.Usage.Observation = &dto.UsageObservation{Source: "provider", Terminal: "unknown"}
			} else {
				observed.Usage.UsageSource = "provider"
			}
			usage, containStreamUsage, usageStreamData = observed.Usage, true, data
		}
		collectStreamFunctionCallNames(data, seenStreamToolCalls, &streamFunctionCallNames)
		if err := processTokenData(info.RelayMode, data, &responseTextBuilder, &toolCount); err != nil {
			sr.Stop(err)
			return
		}
		// Conversion terminals depend on the final usage and scanner outcome.
		// Content is emitted now, so the scanner observes the event we wrote.
		if info.RelayFormat != types.RelayFormatOpenAI {
			if len(observed.Choices) == 0 {
				return
			}
			observed.Usage = nil
			for i := range observed.Choices {
				observed.Choices[i].FinishReason = nil
			}
			encoded, err := common.Marshal(observed)
			if err != nil {
				sr.Stop(err)
				return
			}
			data = string(encoded)
		}
		if err := HandleStreamFormat(c, info, data, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent); err != nil {
			sr.Stop(err)
		}
	})

	// A scanner/adapter error is a failed upstream attempt even when the
	// scanner reached EOF. Returning nil here would mark the channel healthy,
	// settle billing, and suppress bounded failover. If output was already
	// committed, the controller will retain the response boundary and refuse
	// to splice another provider stream.
	streamErr := helper.StreamError(info)

	if lastStreamData == "" && streamErr == nil {
		streamErr = types.NewOpenAIError(fmt.Errorf("empty upstream stream"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	if !containStreamUsage && (responseTextBuilder.Len() > 0 || toolCount > 0 || streamErr == nil) {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += toolCount * 7
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	if usageStreamData == "" {
		usageStreamData = lastStreamData
	}
	applyUsagePostProcessing(info, usage, common.StringToByteSlice(usageStreamData))

	for _, name := range streamFunctionCallNames {
		info.CountBillableToolCall(dto.BuildInCallFunctionCall, name)
	}

	if streamErr != nil {
		return usage, streamErr
	}
	responseId, createAt, model = tail.response.Id, tail.response.Created, tail.response.Model
	systemFingerprint = tail.response.GetSystemFingerprint()
	if info.RelayFormat != types.RelayFormatOpenAI {
		finalData, err := tail.finalData(usage)
		if err != nil {
			return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		lastStreamData = finalData
	}
	if err := HandleFinalResponse(c, info, lastStreamData, responseId, createAt, model, systemFingerprint, usage, containStreamUsage); err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	return usage, nil
}

func collectStreamFunctionCallNames(data string, seen map[string]struct{}, names *[]string) {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
		return
	}
	for _, choice := range streamResponse.Choices {
		for i, tc := range choice.Delta.ToolCalls {
			name := tc.Function.Name
			if name == "" {
				continue
			}
			toolIdx := i
			if tc.Index != nil {
				toolIdx = *tc.Index
			}
			key := fmt.Sprintf("%d-%d", choice.Index, toolIdx)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			*names = append(*names, name)
		}
	}
}

func OpenaiHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	var simpleResponse dto.OpenAITextResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	// Unmarshal to simpleResponse
	if info.ChannelType == constant.ChannelTypeOpenRouter && info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		// 尝试解析为 openrouter enterprise
		var enterpriseResponse openrouter.OpenRouterEnterpriseResponse
		err = common.Unmarshal(responseBody, &enterpriseResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if enterpriseResponse.Success {
			responseBody = enterpriseResponse.Data
		} else {
			logger.LogError(c, "openrouter enterprise response success=false")
			return nil, types.NewOpenAIError(fmt.Errorf("openrouter response success=false"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	}

	err = common.Unmarshal(responseBody, &simpleResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	info.ObserveResponseModel(simpleResponse.Model)
	info.ObserveActualServiceTier(simpleResponse.ServiceTier)
	if oaiError := simpleResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		c.Set("response_termination", types.ResponseTermination{Kind: types.ResponseFailed})
		return &simpleResponse.Usage, types.WithOpenAIError(*oaiError, http.StatusBadGateway)
	}
	if info.RelayMode == relayconstant.RelayModeEmbeddings {
		if err := helper.ValidateEmbeddingResponse(responseBody, helper.EmbeddingInputCount(info.Request)); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if err := helper.ValidateVectorUsage(&simpleResponse.Usage); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if !dto.HasJSONTokenCounts([]byte(gjson.GetBytes(responseBody, "usage").Raw), "prompt_tokens") {
			simpleResponse.Usage = *service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else {
			simpleResponse.Usage.UsageSource = "provider"
		}
		return &simpleResponse.Usage, helper.WriteVectorResponse(c, resp, info, responseBody)
	}
	originalTotal := simpleResponse.Usage.TotalTokens
	if err := service.NormalizeProviderUsage(&simpleResponse.Usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	for _, choice := range simpleResponse.Choices {
		if choice.FinishReason == constant.FinishReasonContentFilter {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_finish_reason=content_filter")
			break
		}
	}

	for _, choice := range simpleResponse.Choices {
		for _, tc := range choice.Message.ParseToolCalls() {
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, tc.Function.Name)
		}
	}

	forceFormat := false
	if info.ChannelSetting.ForceFormat {
		forceFormat = true
	}

	usageModified := originalTotal != simpleResponse.Usage.TotalTokens
	rawPrompt := gjson.GetBytes(responseBody, "usage.prompt_tokens")
	rawCompletion := gjson.GetBytes(responseBody, "usage.completion_tokens")
	missingPrompt := !rawPrompt.Exists() || rawPrompt.Type == gjson.Null
	missingCompletion := !rawCompletion.Exists() || rawCompletion.Type == gjson.Null
	if missingPrompt {
		simpleResponse.Usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	if missingCompletion {
		for _, choice := range simpleResponse.Choices {
			simpleResponse.Usage.CompletionTokens += tokenkit.Count(info.UpstreamModelName, choice.Message.StringContent()+choice.Message.GetReasoningContent())
		}
	}
	if missingPrompt || missingCompletion {
		simpleResponse.Usage.TotalTokens = simpleResponse.Usage.PromptTokens + simpleResponse.Usage.CompletionTokens
		simpleResponse.Usage.UsageSource = "estimated"
		usageModified = true
	} else {
		simpleResponse.Usage.UsageSource = "provider"
	}
	if err := service.NormalizeProviderUsage(&simpleResponse.Usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	applyUsagePostProcessing(info, &simpleResponse.Usage, responseBody)
	c.Set("response_termination", relayconvert.TerminationFromResponse(&simpleResponse))

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if usageModified {
			var bodyMap map[string]interface{}
			err = common.Unmarshal(responseBody, &bodyMap)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			}
			bodyMap["usage"] = simpleResponse.Usage
			responseBody, _ = common.Marshal(bodyMap)
		}
		if forceFormat {
			responseBody, err = common.Marshal(simpleResponse)
			if err != nil {
				return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
			}
		} else {
			break
		}
	case types.RelayFormatClaude:
		convertResult, err := relayconvert.ConvertResponse(c, info, types.RelayFormatClaude, &simpleResponse)
		if err != nil {
			return &simpleResponse.Usage, helper.ResponseConversionError(err)
		}
		helper.ObserveResponseConversion(c, *convertResult)
		claudeRespStr, err := common.Marshal(convertResult.Value)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		convertResult, err := relayconvert.ConvertResponse(c, info, types.RelayFormatGemini, &simpleResponse)
		if err != nil {
			return &simpleResponse.Usage, helper.ResponseConversionError(err)
		}
		helper.ObserveResponseConversion(c, *convertResult)
		geminiRespStr, err := common.Marshal(convertResult.Value)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = geminiRespStr
	}

	err = c.Request.Context().Err()
	if err == nil {
		_, err = service.CopyResponseBytes(c, resp, responseBody)
	}
	if err != nil {
		if _, failed := c.Get(service.GatewayStateOutputErrorKey); failed {
			return &simpleResponse.Usage, types.NewOpenAIError(err, types.ErrorCodeUpdateDataError, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return &simpleResponse.Usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	info.MarkValidOutput()
	return &simpleResponse.Usage, nil
}
