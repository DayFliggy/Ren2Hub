package ollama

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type ollamaChatStreamChunk struct {
	Error     string `json:"error"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	// chat
	Message *struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		Thinking  json.RawMessage  `json:"thinking"`
		ToolCalls []OllamaToolCall `json:"tool_calls"`
	} `json:"message"`
	// generate
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	DoneReason         string `json:"done_reason"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	EvalCount          int    `json:"eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalDuration       int64  `json:"eval_duration"`
}

func ollamaToolCallsToOpenAI(toolCalls []OllamaToolCall, responseID string, startIndex int, includeIndex bool) ([]dto.ToolCallResponse, int, error) {
	if len(toolCalls) == 0 {
		return nil, startIndex, nil
	}
	result := make([]dto.ToolCallResponse, 0, len(toolCalls))
	for _, tc := range toolCalls {
		var arguments map[string]json.RawMessage
		if err := common.Unmarshal(tc.Function.Arguments, &arguments); err != nil || arguments == nil || strings.TrimSpace(tc.Function.Name) == "" {
			return nil, startIndex, fmt.Errorf("ollama tool call must name a function with JSON object arguments")
		}
		toolCallID := tc.ID
		if toolCallID == "" {
			toolCallID = fmt.Sprintf("call_%s_%d", responseID, startIndex)
		}
		tr := dto.ToolCallResponse{
			ID:   toolCallID,
			Type: "function",
			Function: dto.FunctionResponse{
				Name:      tc.Function.Name,
				Arguments: string(tc.Function.Arguments),
			},
		}
		if includeIndex {
			tr.SetIndex(startIndex)
		}
		startIndex++
		result = append(result, tr)
	}
	return result, startIndex, nil
}

func toUnix(ts string) int64 {
	if ts == "" {
		return time.Now().Unix()
	}
	// try time.RFC3339 or with nanoseconds
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339, ts)
		if err2 == nil {
			return t2.Unix()
		}
		return time.Now().Unix()
	}
	return t.Unix()
}

func ollamaStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("empty response"), types.ErrorCodeBadResponse, http.StatusBadRequest)
	}
	defer service.CloseResponseBodyGracefully(resp)

	target := info.RelayFormat
	if target == "" {
		target = types.RelayFormatOpenAI
	}
	responseID, created := helper.GetResponseID(c), time.Now().Unix()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAI, target, relayconvert.ResponseStreamOptions{EmitSequenceNumber: true,
		ID: responseID, Created: created, Model: info.UpstreamModelName, IncludeUsage: info.ShouldIncludeUsage,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	var usage *dto.Usage
	var toolCallIndex int
	var deliveredText strings.Builder
	helper.NDJSONStreamScannerHandler(c, resp, info, func(line string, result *helper.StreamResult) {
		var chunk ollamaChatStreamChunk
		if err := common.Unmarshal([]byte(line), &chunk); err != nil {
			result.Stop(err)
			return
		}
		if chunk.Error != "" {
			result.Stop(fmt.Errorf("ollama upstream error: %s", chunk.Error))
			return
		}
		info.ObserveResponseModel(chunk.Model)
		delta := dto.ChatCompletionsStreamResponse{
			Id: responseID, Object: "chat.completion.chunk", Created: created, Model: info.UpstreamModelName,
			Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}}},
		}
		content, thinking := chunk.Response, ""
		if chunk.Message != nil {
			content = chunk.Message.Content
			if len(chunk.Message.Thinking) > 0 && string(chunk.Message.Thinking) != "null" {
				if err := common.Unmarshal(chunk.Message.Thinking, &thinking); err != nil {
					result.Stop(fmt.Errorf("invalid ollama thinking: %w", err))
					return
				}
			}
			delta.Choices[0].Delta.ToolCalls, toolCallIndex, err = ollamaToolCallsToOpenAI(chunk.Message.ToolCalls, responseID, toolCallIndex, true)
			if err != nil {
				result.Stop(err)
				return
			}
		}
		delta.Choices[0].Delta.SetContentString(content)
		delta.Choices[0].Delta.SetReasoningContent(thinking)
		if chunk.Done {
			usage, err = ollamaChunkUsage(chunk)
			if err != nil {
				result.Stop(err)
				return
			}
			finishReason := chunk.DoneReason
			if finishReason == "" {
				finishReason = "stop"
			}
			if toolCallIndex > 0 {
				finishReason = constant.FinishReasonToolCalls
			}
			delta.Choices[0].FinishReason = &finishReason
			delta.Usage = usage
			state.SetUsage(usage)
		}
		converted, err := relayconvert.ConvertStreamResponseChunk(c, info, state, &delta)
		if err != nil {
			result.Stop(err)
			return
		}
		for _, event := range converted {
			if err := helper.WriteConvertedStreamResult(c, event); err != nil {
				result.Stop(err)
				return
			}
		}
		if content != "" || thinking != "" || len(delta.Choices[0].Delta.ToolCalls) > 0 {
			result.MarkValidOutput()
			deliveredText.WriteString(content)
			deliveredText.WriteString(thinking)
			for _, tool := range delta.Choices[0].Delta.ToolCalls {
				deliveredText.WriteString(tool.Function.Arguments)
			}
		}
		if chunk.Done {
			result.Done()
		}
	})
	if usage == nil && info.HasValidOutput() {
		usage = service.ResponseText2Usage(c, deliveredText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.SetUsage(usage)
	}
	if err := helper.FinalizeConvertedStream(c, info, state); err != nil {
		return usage, err
	}
	if target == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}

func ollamaChunkUsage(chunk ollamaChatStreamChunk) (*dto.Usage, error) {
	if chunk.PromptEvalCount < 0 || chunk.EvalCount < 0 || chunk.PromptEvalCount > math.MaxInt-chunk.EvalCount {
		return nil, fmt.Errorf("invalid ollama token usage")
	}
	return &dto.Usage{PromptTokens: chunk.PromptEvalCount, CompletionTokens: chunk.EvalCount, TotalTokens: chunk.PromptEvalCount + chunk.EvalCount, UsageSource: "provider"}, nil
}

// non-stream handler for chat/generate
func ollamaChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("empty ollama response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	var single ollamaChatStreamChunk
	var chunks []ollamaChatStreamChunk
	if err := common.Unmarshal(body, &single); err == nil {
		chunks = append(chunks, single)
	} else {
		scanner := helper.NewStreamScanner(strings.NewReader(string(body)))
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "" {
				continue
			}
			var chunk ollamaChatStreamChunk
			if err := common.UnmarshalJsonStr(scanner.Text(), &chunk); err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			chunks = append(chunks, chunk)
		}
		if err := scanner.Err(); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
		}
	}
	if len(chunks) == 0 || !chunks[len(chunks)-1].Done {
		return nil, types.NewOpenAIError(io.ErrUnexpectedEOF, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	responseID := common.GetUUID()
	var (
		aggContent       strings.Builder
		reasoningBuilder strings.Builder
		lastChunk        ollamaChatStreamChunk
		toolCallIndex    int
		toolCalls        []dto.ToolCallResponse
	)
	for index, ck := range chunks {
		if ck.Error != "" || (ck.Done && index != len(chunks)-1) {
			return nil, types.NewOpenAIError(fmt.Errorf("invalid ollama completion"), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		lastChunk = ck
		info.ObserveResponseModel(ck.Model)
		if ck.Message != nil && len(ck.Message.Thinking) > 0 {
			raw := strings.TrimSpace(string(ck.Message.Thinking))
			if raw != "" && raw != "null" {
				var thinkingContent string
				if err := common.Unmarshal(ck.Message.Thinking, &thinkingContent); err != nil {
					return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
				reasoningBuilder.WriteString(thinkingContent)
			}
		}
		if ck.Message != nil && ck.Message.Content != "" {
			aggContent.WriteString(ck.Message.Content)
		} else if ck.Response != "" {
			aggContent.WriteString(ck.Response)
		}
		if ck.Message != nil && len(ck.Message.ToolCalls) > 0 {
			var converted []dto.ToolCallResponse
			converted, toolCallIndex, err = ollamaToolCallsToOpenAI(ck.Message.ToolCalls, responseID, toolCallIndex, false)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			toolCalls = append(toolCalls, converted...)
		}
	}

	model := lastChunk.Model
	if model == "" {
		model = info.UpstreamModelName
	}
	created := toUnix(lastChunk.CreatedAt)
	usage, err := ollamaChunkUsage(lastChunk)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	content := aggContent.String()
	finishReason := lastChunk.DoneReason
	if finishReason == "" {
		finishReason = "stop"
	}
	if len(toolCalls) > 0 {
		finishReason = constant.FinishReasonToolCalls
	}

	msg := dto.Message{Role: "assistant", Content: content}
	if len(toolCalls) > 0 {
		msg.ToolCalls, err = common.Marshal(toolCalls)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
	}
	if rc := reasoningBuilder.String(); rc != "" {
		msg.ReasoningContent = &rc
	}
	full := dto.OpenAITextResponse{
		Id:      responseID,
		Model:   model,
		Object:  "chat.completion",
		Created: created,
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: *usage,
	}
	var output any = &full
	if info.RelayFormat != "" && info.RelayFormat != types.RelayFormatOpenAI {
		converted, err := relayconvert.ConvertResponse(c, info, info.RelayFormat, &full)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		output = converted.Value
	}
	out, err := common.Marshal(output)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	resp.Header.Set("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, resp, out)
	return usage, nil
}
