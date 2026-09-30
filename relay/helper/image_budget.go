package helper

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// FinalImagePriceMeta reads the actual serialized attempt, after adaptor and
// channel overrides. It is a quantity budget, not a claim of reliable image TPM.
func FinalImagePriceMeta(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*types.TokenCountMeta, error) {
	if body == nil {
		return nil, fmt.Errorf("final image request is unavailable")
	}
	reader := bufio.NewReader(body)
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if !strings.ContainsRune(" \t\r\n", rune(b)) {
			if err := reader.UnreadByte(); err != nil {
				return nil, err
			}
			break
		}
	}
	first, err := reader.Peek(1)
	if err != nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if first[0] == '{' {
		if err := common.DecodeJson(reader, &payload); err != nil {
			return nil, fmt.Errorf("invalid final image request: %w", err)
		}
	} else {
		mediaType, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
			return nil, fmt.Errorf("invalid final image request format")
		}
		payload = make(map[string]json.RawMessage)
		form := multipart.NewReader(reader, params["boundary"])
		for {
			part, err := form.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			name := part.FormName()
			if part.FileName() != "" || (name != "n" && name != "size" && name != "quality" && name != "service_tier") {
				continue
			}
			if _, exists := payload[name]; exists {
				return nil, fmt.Errorf("duplicate image form field %s", name)
			}
			value, err := io.ReadAll(io.LimitReader(part, 65537))
			if err != nil || len(value) > 65536 {
				return nil, fmt.Errorf("invalid image form field %s", name)
			}
			if name == "n" {
				payload[name] = value
			} else {
				payload[name], err = common.Marshal(string(value))
				if err != nil {
					return nil, err
				}
			}
		}
	}
	fields, countField, required := payload, "n", false
	switch info.ApiType {
	case constant.APITypeAli, constant.APITypeGemini, constant.APITypeVertexAi, constant.APITypeReplicate:
		container := "parameters"
		if info.ApiType == constant.APITypeReplicate {
			container, countField = "input", "num_outputs"
		} else if info.ApiType != constant.APITypeAli {
			countField = "sampleCount"
		}
		if err := common.Unmarshal(payload[container], &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("invalid image %s", container)
		}
		required = true
	case constant.APITypeSiliconFlow:
		countField = "batch_size"
	case constant.APITypeJimeng:
		// This adaptor exposes one generated image, without a batch option.
		countField = ""
	}
	count := uint(1)
	if raw, exists := fields[countField]; exists {
		if string(raw) == "null" || common.Unmarshal(raw, &count) != nil || count == 0 || count > dto.MaxImageN {
			return nil, fmt.Errorf("image %s must be an integer between 1 and %d", countField, dto.MaxImageN)
		}
	} else if required {
		return nil, fmt.Errorf("image %s must be explicit after parameter overrides", countField)
	}
	if info.ApiType == constant.APITypeVolcEngine {
		var options struct {
			MaxImages *uint `json:"max_images"`
		}
		if raw, exists := payload["sequential_image_generation_options"]; exists {
			if err := common.Unmarshal(raw, &options); err != nil || options.MaxImages != nil && (*options.MaxImages == 0 || *options.MaxImages > dto.MaxImageN) {
				return nil, fmt.Errorf("invalid sequential image count")
			}
			if options.MaxImages != nil && *options.MaxImages > count {
				count = *options.MaxImages
			}
		}
	}
	request := dto.ImageRequest{Model: info.BillingModelName(), N: &count}
	for name, target := range map[string]*string{"size": &request.Size, "quality": &request.Quality} {
		if raw, exists := fields[name]; exists {
			if err := common.Unmarshal(raw, target); err != nil {
				return nil, fmt.Errorf("invalid image %s", name)
			}
		}
	}
	request.Size = strings.ReplaceAll(request.Size, "*", "x")
	if source, ok := info.Request.(*dto.ImageRequest); ok && source.BillingParameters != nil {
		parameters := *source.BillingParameters
		request.BillingParameters = &parameters
	}
	if info.ApiType == constant.APITypeAli {
		request.BillingParameters = &dto.ImageBillingParameters{N: &count}
		if raw, exists := fields["prompt_extend"]; exists {
			if err := common.Unmarshal(raw, &request.BillingParameters.PromptExtend); err != nil {
				return nil, fmt.Errorf("invalid image prompt_extend")
			}
		}
	}
	// Preserve the final wire pricing conditions while normalizing provider-
	// specific quantity fields. Dropping service_tier here would underquote.
	for name, value := range map[string]any{"quantity": count, "quality": request.Quality, "resolution": request.Size} {
		payload[name], err = common.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	pricingPayload, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := FreezeOutboundBillingAttributes(info, pricingPayload); err != nil {
		return nil, err
	}
	input, err := BuildBillingExprRequestInputFromRequest(&request, info.RequestHeaders)
	if err != nil {
		return nil, err
	}
	input.Body, err = imageBillingBody(&request, int(count))
	if err != nil {
		return nil, err
	}
	input.Attributes = info.BillingRequestInput.Attributes
	imageCount := int(count)
	input.ImageCount = &imageCount
	info.BillingRequestInput = &input
	meta := request.GetTokenCountMeta()
	if info.ApiType == constant.APITypeAli && strings.Contains(info.UpstreamModelName, "z-image") {
		var extend bool
		if raw, exists := fields["prompt_extend"]; exists && common.Unmarshal(raw, &extend) != nil {
			return nil, fmt.Errorf("invalid image prompt_extend")
		}
		if extend {
			meta.BillingQuantities["prompt_extend"] = "2"
		}
	}
	return meta, nil
}
