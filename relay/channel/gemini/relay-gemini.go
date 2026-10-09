package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func buildUsageFromGeminiMetadata(metadata *dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.Usage {
	usage := relayconvert.UsageFromGeminiMetadata(metadata, fallbackPromptTokens)
	if usage == nil {
		return dto.Usage{}
	}
	return *usage
}

func attachEstimatedGeminiBillingUsage(usage *dto.Usage) *dto.Usage {
	if usage != nil && usage.BillingUsage == nil {
		usage.BillingUsage = dto.NewEstimatedGeminiChatBillingUsage(usage)
	}
	return usage
}

// patchGeminiZeroCompletionUsage estimates completion tokens locally when upstream
// usageMetadata was billable but reported zero completion tokens even though output
// content was actually received. Typical case: the client aborts a stream before the
// final chunk that carries candidatesTokenCount, leaving prompt-only metadata; without
// this patch the output side would settle at zero quota.
func patchGeminiZeroCompletionUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, responseText string, imageCount int) {
	if usage == nil || usage.CompletionTokens > 0 {
		return
	}
	if responseText == "" && imageCount == 0 {
		return
	}
	estimated := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, usage.PromptTokens)
	usage.CompletionTokens = estimated.CompletionTokens
	if imageCount != 0 && usage.CompletionTokens == 0 {
		usage.CompletionTokens = imageCount * 1400
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	if usage.BillingUsage != nil {
		usage.BillingUsage = dto.CloneBillingUsageWithEstimatedCompletion(usage.BillingUsage, usage.CompletionTokens)
	} else {
		attachEstimatedGeminiBillingUsage(usage)
	}
}

func geminiResponseUsageText(response *dto.GeminiChatResponse) string {
	if response == nil {
		return ""
	}
	var text strings.Builder
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				text.WriteString(part.Text)
			}
			// Function calls are most of an agent turn's output.
			if part.FunctionCall != nil {
				text.WriteString(part.FunctionCall.FunctionName)
				args, _ := common.Marshal(part.FunctionCall.Arguments)
				text.Write(args)
			}
		}
	}
	return text.String()
}

// geminiGroundingCounter accumulates Google Search grounding across one
// upstream response (every stream frame and candidate). Gemini 3 bills each
// unique non-empty search query ("we ignore the empty web search queries when
// counting unique queries", ai.google.dev/gemini-api/docs/google-search);
// Gemini 2.5 and older bill once per grounded prompt, counted here when the
// response carries any search query. Image-search queries count like
// web-search queries; Google documents no separate rule. Google documents no
// response field holding the billed count, so the unique query set is the best
// available evidence, not a guaranteed match.
type geminiGroundingCounter struct {
	queries map[string]struct{}
}

func (g *geminiGroundingCounter) observe(response *dto.GeminiChatResponse) {
	if response == nil {
		return
	}
	for _, candidate := range response.Candidates {
		metadata := candidate.GroundingMetadata
		if metadata == nil {
			continue
		}
		for _, query := range slices.Concat(metadata.WebSearchQueries, metadata.ImageSearchQueries) {
			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}
			if g.queries == nil {
				g.queries = make(map[string]struct{})
			}
			g.queries[query] = struct{}{}
		}
	}
}

// commit records google_search as the unique query count. Ren2Hub seeds no
// tool prices, so the grounded-prompt key bills only when the frozen quote
// prices it: an operator prices google_search_grounded_prompt (for example
// with a gemini-2.5* prefix) and sets google_search to 0 for the models
// Google bills per grounded prompt.
func (g *geminiGroundingCounter) commit(info *relaycommon.RelayInfo) {
	if info == nil || len(g.queries) == 0 {
		return
	}
	info.SetBillableToolCount(dto.BuildInToolGoogleSearch, len(g.queries))
	if info.QuotePricesTool(relaycommon.GoogleSearchGroundedPromptTool) {
		info.SetBillableToolCount(relaycommon.GoogleSearchGroundedPromptTool, 1)
	}
}

// countGeminiResponseGrounding bills the grounding of one non-stream response.
func countGeminiResponseGrounding(info *relaycommon.RelayInfo, response *dto.GeminiChatResponse) {
	var grounding geminiGroundingCounter
	grounding.observe(response)
	grounding.commit(info)
}

func buildUsageFromGeminiResponse(c *gin.Context, info *relaycommon.RelayInfo, response *dto.GeminiChatResponse) dto.Usage {
	metadata := response.GetUsageMetadata()
	if dto.HasGeminiUsageMetadataTokens(metadata) {
		usage := buildUsageFromGeminiMetadata(metadata, info.GetEstimatePromptTokens())
		patchGeminiZeroCompletionUsage(c, info, &usage, geminiResponseUsageText(response), geminiResponseInlineImageCount(response))
		return usage
	}
	usage := service.ResponseText2Usage(c, geminiResponseUsageText(response), info.UpstreamModelName, info.GetEstimatePromptTokens())
	attachEstimatedGeminiBillingUsage(usage)
	return *usage
}

func geminiResponseInlineImageCount(response *dto.GeminiChatResponse) int {
	if response == nil {
		return 0
	}
	count := 0
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && part.InlineData.MimeType != "" {
				count++
			}
		}
	}
	return count
}

func responseGeminiChat2OpenAI(c *gin.Context, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	return relayconvert.ResponseGeminiChat2OpenAI(helper.GetResponseID(c), common.GetTimestamp(), response)
}

func streamResponseGeminiChat2OpenAI(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool) {
	return relayconvert.StreamResponseGeminiChat2OpenAI(geminiResponse)
}

func handleStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	err = openai.HandleStreamFormat(c, info, string(streamData), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
	if err != nil {
		return fmt.Errorf("failed to handle stream format: %w", err)
	}
	return nil
}

func handleFinalStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	return openai.HandleFinalResponse(c, info, string(streamData), resp.Id, resp.Created, resp.Model, resp.GetSystemFingerprint(), resp.Usage, false)
}

func geminiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, callback func(data string, geminiResponse *dto.GeminiChatResponse) bool) (*dto.Usage, *types.NewAPIError) {
	if contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); contentType == "application/json" {
		_, _, apiErr := readGeminiGenerationResponse(c, resp)
		if apiErr == nil {
			apiErr = types.NewOpenAIError(errors.New("Gemini API returned JSON for a streaming request"), types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, apiErr)
		return nil, apiErr
	}
	var usage = &dto.Usage{}
	var imageCount int
	var hasBillableUsageMetadata bool
	var sawOutput, sawTerminal bool
	var accumulatedUsageMetadata *dto.GeminiUsageMetadata
	var grounding geminiGroundingCounter
	responseText := strings.Builder{}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		geminiResponse, apiErr := parseGeminiGenerationResponse(c, []byte(data), true, sawOutput)
		if geminiResponse != nil {
			info.ObserveResponseModel(gjson.Get(data, "modelVersion").Str)
		}
		if apiErr != nil {
			apiErr.RetryAfter, _ = service.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			sr.Stop(apiErr)
			return
		}
		sawOutput = sawOutput || geminiResponseHasOutput(geminiResponse)
		for _, candidate := range geminiResponse.Candidates {
			sawTerminal = sawTerminal || (candidate.FinishReason != nil && geminiKnownFinishReason(*candidate.FinishReason))
		}

		// Commit on every frame so a client abort keeps the queries seen so far.
		grounding.observe(geminiResponse)
		grounding.commit(info)

		// 统计图片数量
		for _, candidate := range geminiResponse.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil && part.InlineData.MimeType != "" {
					imageCount++
				}
			}
		}
		responseText.WriteString(geminiResponseUsageText(geminiResponse))

		// 更新使用量统计
		if metadata := geminiResponse.GetUsageMetadata(); dto.HasGeminiUsageMetadataTokens(metadata) {
			accumulatedUsageMetadata = dto.MergeGeminiUsageMetadataNonZero(accumulatedUsageMetadata, metadata)
			mappedUsage := buildUsageFromGeminiMetadata(accumulatedUsageMetadata, info.GetEstimatePromptTokens())
			*usage = mappedUsage
			hasBillableUsageMetadata = true
		}

		if !sawOutput && !sawTerminal {
			return
		}
		if !callback(data, geminiResponse) {
			sr.Stop(fmt.Errorf("gemini callback stopped"))
		}
	})
	if !sawOutput && !sawTerminal && helper.StreamError(info) == nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, geminiEmptyResponseError())
	}

	if !hasBillableUsageMetadata {
		if responseText.Len() > 0 || imageCount > 0 {
			usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else {
			usage = &dto.Usage{}
		}
		if imageCount != 0 && usage.CompletionTokens == 0 {
			usage.CompletionTokens = imageCount * 1400
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		}
		attachEstimatedGeminiBillingUsage(usage)
	} else {
		patchGeminiZeroCompletionUsage(c, info, usage, responseText.String(), imageCount)
	}

	return usage, helper.StreamError(info)
}

func GeminiChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	return geminiConvertedStreamHandler(c, info, resp, info.RelayFormat)
}

func GeminiChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, geminiResponse, apiErr := readGeminiGenerationResponse(c, resp)
	if apiErr != nil {
		return nil, apiErr
	}
	info.ObserveResponseModel(gjson.GetBytes(responseBody, "modelVersion").Str)
	countGeminiResponseGrounding(info, geminiResponse)
	usage := buildUsageFromGeminiResponse(c, info, geminiResponse)
	switch info.RelayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatClaude:
		convertResult, err := relayconvert.ConvertResponse(c, info, info.RelayFormat, geminiResponse)
		if err != nil {
			return &usage, helper.ResponseConversionError(err)
		}
		helper.ObserveResponseConversion(c, *convertResult)
		if output, ok := convertResult.Value.(*dto.OpenAITextResponse); ok {
			output.Model, output.Usage = info.UpstreamModelName, usage
		}
		encoded, err := common.Marshal(convertResult.Value)
		if err != nil {
			return &usage, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = encoded
	case types.RelayFormatGemini:
		break
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

func GeminiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if err := helper.ValidateGeminiEmbeddingResponse(responseBody, helper.EmbeddingInputCount(info.Request), true); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	var geminiResponse dto.GeminiBatchEmbeddingResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(geminiResponse.Embeddings)),
		Model:  info.UpstreamModelName,
	}

	for i, embedding := range geminiResponse.Embeddings {
		openAIResponse.Data = append(openAIResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    "embedding",
			Embedding: embedding.Values,
			Index:     i,
		})
	}

	// calculate usage
	// https://ai.google.dev/gemini-api/docs/pricing?hl=zh-cn#text-embedding-004
	// Google has not yet clarified how embedding models will be billed
	// refer to openai billing method to use input tokens billing
	// https://platform.openai.com/docs/guides/embeddings#what-are-embeddings
	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	openAIResponse.Usage = *usage

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	return usage, helper.WriteVectorResponse(c, resp, info, jsonResponse)
}

func GeminiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var geminiResponse dto.GeminiImageResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if len(geminiResponse.Predictions) == 0 {
		return nil, types.NewOpenAIError(errors.New("no images generated"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, len(geminiResponse.Predictions)),
	}

	for _, prediction := range geminiResponse.Predictions {
		if prediction.RaiFilteredReason != "" {
			continue // skip filtered image
		}
		openAIResponse.Data = append(openAIResponse.Data, dto.ImageData{
			B64Json: prediction.BytesBase64Encoded,
		})
	}

	// https://github.com/google-gemini/cookbook/blob/719a27d752aac33f39de18a8d3cb42a70874917e/quickstarts/Counting_Tokens.ipynb
	// each image has fixed 258 tokens
	const imageTokens = 258
	generatedImages := len(openAIResponse.Data)

	usage := &dto.Usage{
		PromptTokens:     imageTokens * generatedImages, // each generated image has fixed 258 tokens
		CompletionTokens: 0,                             // image generation does not calculate completion tokens
		TotalTokens:      imageTokens * generatedImages,
		UsageSource:      "estimated",
	}

	return openai.DeliverConvertedImage(c, info, resp, &openAIResponse, usage)
}

type GeminiModelsResponse struct {
	Models        []dto.GeminiModel `json:"models"`
	NextPageToken string            `json:"nextPageToken"`
}

func FetchGeminiModels(baseURL, apiKey, proxyURL string) ([]string, error) {
	client, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP客户端失败: %v", err)
	}

	allModels := make([]string, 0)
	nextPageToken := ""
	maxPages := 100 // Safety limit to prevent infinite loops

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/v1beta/models", baseURL)
		if nextPageToken != "" {
			url = fmt.Sprintf("%s?pageToken=%s", url, nextPageToken)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("创建请求失败: %v", err)
		}

		request.Header.Set("x-goog-api-key", apiKey)

		response, err := client.Do(request)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("请求失败: %v", err)
		}

		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			cancel()
			return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, string(body))
		}

		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("读取响应失败: %v", err)
		}

		var modelsResponse GeminiModelsResponse
		if err = common.Unmarshal(body, &modelsResponse); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}

		for _, model := range modelsResponse.Models {
			modelNameValue, ok := model.Name.(string)
			if !ok {
				continue
			}
			modelName := strings.TrimPrefix(modelNameValue, "models/")
			allModels = append(allModels, modelName)
		}

		nextPageToken = modelsResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}

	return allModels, nil
}
