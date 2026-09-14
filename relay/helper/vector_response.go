package helper

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// EmbeddingInputCount treats a token array as one input, not one vector per token.
func EmbeddingInputCount(request dto.Request) int {
	switch request := request.(type) {
	case *dto.EmbeddingRequest:
		if request == nil {
			return 0
		}
		switch input := request.Input.(type) {
		case string:
			return 1
		case []string:
			return len(input)
		case []int:
			if len(input) > 0 {
				return 1
			}
		case []any:
			if len(input) == 0 {
				return 0
			}
			switch input[0].(type) {
			case float64, int, json.Number:
				return 1
			}
			return len(input)
		}
	case *dto.GeminiEmbeddingRequest:
		return 1
	case *dto.GeminiBatchEmbeddingRequest:
		if request != nil {
			return len(request.Requests)
		}
	}
	return 0
}

func ValidateEmbeddingResponse(body []byte, expectedCount int) error {
	var response struct {
		Data []struct {
			Index     *int            `json:"index"`
			Embedding json.RawMessage `json:"embedding"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("invalid embedding response: %w", err)
	}
	vectors := make([]json.RawMessage, len(response.Data))
	seen := make(map[int]bool, len(response.Data))
	for i, item := range response.Data {
		if item.Index == nil || *item.Index < 0 || *item.Index >= len(response.Data) || seen[*item.Index] {
			return fmt.Errorf("invalid or repeated embedding index")
		}
		seen[*item.Index] = true
		vectors[i] = item.Embedding
	}
	return ValidateEmbeddingVectors(vectors, expectedCount)
}

func ValidateGeminiEmbeddingResponse(body []byte, expectedCount int, batch bool) error {
	type vector struct {
		Values json.RawMessage `json:"values"`
	}
	var response struct {
		Embedding  vector   `json:"embedding"`
		Embeddings []vector `json:"embeddings"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("invalid embedding response: %w", err)
	}
	if !batch {
		return ValidateEmbeddingVectors([]json.RawMessage{response.Embedding.Values}, expectedCount)
	}
	vectors := make([]json.RawMessage, len(response.Embeddings))
	for i, embedding := range response.Embeddings {
		vectors[i] = embedding.Values
	}
	return ValidateEmbeddingVectors(vectors, expectedCount)
}

func ValidateEmbeddingVectors(vectors []json.RawMessage, expectedCount int) error {
	if len(vectors) == 0 || (expectedCount > 0 && len(vectors) != expectedCount) {
		return fmt.Errorf("embedding response count does not match the input")
	}
	dimensions := 0
	for _, raw := range vectors {
		var values []*float64
		if len(raw) > 0 && raw[0] == '"' {
			var encoded string
			if err := common.Unmarshal(raw, &encoded); err != nil {
				return fmt.Errorf("invalid embedding encoding")
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || len(decoded) == 0 || len(decoded)%4 != 0 {
				return fmt.Errorf("invalid base64 embedding")
			}
			values = make([]*float64, len(decoded)/4)
			for i := range values {
				value := float64(math.Float32frombits(binary.LittleEndian.Uint32(decoded[i*4:])))
				values[i] = &value
			}
		} else if err := common.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("invalid embedding vector: %w", err)
		}
		if len(values) == 0 || (dimensions != 0 && dimensions != len(values)) {
			return fmt.Errorf("empty or inconsistent embedding dimensions")
		}
		dimensions = len(values)
		for _, value := range values {
			if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
				return fmt.Errorf("invalid embedding value")
			}
		}
	}
	return nil
}

// ValidateRerankResponse runs before conversion so missing fields do not become valid zero values.
func ValidateRerankResponse(body []byte, documentCount int) error {
	var response struct {
		Results []struct {
			Index *int     `json:"index"`
			Score *float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("invalid rerank response: %w", err)
	}
	if len(response.Results) == 0 || len(response.Results) > documentCount {
		return fmt.Errorf("invalid rerank result count")
	}
	seen := make(map[int]bool, len(response.Results))
	for _, result := range response.Results {
		if result.Index == nil || *result.Index < 0 || *result.Index >= documentCount || seen[*result.Index] {
			return fmt.Errorf("invalid or repeated rerank index")
		}
		if result.Score == nil || math.IsNaN(*result.Score) || math.IsInf(*result.Score, 0) {
			return fmt.Errorf("invalid rerank relevance score")
		}
		seen[*result.Index] = true
	}
	return nil
}

func ValidateVectorUsage(usage *dto.Usage) error {
	if usage == nil {
		return fmt.Errorf("missing vector response usage")
	}
	counts := []int{usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.InputTokens, usage.OutputTokens,
		usage.PromptCacheHitTokens, usage.ClaudeCacheCreation5mTokens, usage.ClaudeCacheCreation1hTokens,
		usage.CompletionTokenDetails.TextTokens, usage.CompletionTokenDetails.AudioTokens, usage.CompletionTokenDetails.ImageTokens, usage.CompletionTokenDetails.ReasoningTokens}
	for _, details := range []*dto.InputTokenDetails{&usage.PromptTokensDetails, usage.InputTokensDetails} {
		if details != nil {
			counts = append(counts, details.CachedTokens, details.CachedCreationTokens, details.CacheWriteTokens, details.TextTokens, details.AudioTokens, details.ImageTokens)
		}
	}
	for _, count := range counts {
		if count < 0 || count > math.MaxInt32 {
			return fmt.Errorf("invalid vector response usage")
		}
	}
	if int64(usage.PromptTokens)+int64(usage.CompletionTokens) > math.MaxInt32 || int64(usage.InputTokens)+int64(usage.OutputTokens) > math.MaxInt32 {
		return fmt.Errorf("invalid vector response usage total")
	}
	if usage.TotalTokens > 0 && (int64(usage.TotalTokens) < int64(usage.PromptTokens)+int64(usage.CompletionTokens) || int64(usage.TotalTokens) < int64(usage.InputTokens)+int64(usage.OutputTokens)) {
		return fmt.Errorf("inconsistent vector response usage")
	}
	usage.BillingUsage = nil
	return nil
}

func WriteVectorResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, body []byte) *types.NewAPIError {
	n, err := service.CopyResponseBytes(c, resp, body)
	if err == nil && n != len(body) {
		err = io.ErrShortWrite
	}
	if err != nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	info.MarkValidOutput()
	return nil
}
