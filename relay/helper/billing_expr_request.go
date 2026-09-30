package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// BillingAttributesFromBody projects only registered scalar attributes. Request
// routing checks. Full expression inputs are persisted only as encrypted evidence.
func BillingAttributesFromBody(body []byte) (map[string]interface{}, error) {
	return billingAttributesFromBody(body, "")
}

func billingAttributesFromBody(body []byte, format types.RelayFormat) (map[string]interface{}, error) {
	attrs := map[string]interface{}{}
	if len(body) == 0 {
		return attrs, nil
	}
	var fields map[string]interface{}
	if err := common.DecodeJsonNumbers(bytes.NewReader(body), &fields); err != nil {
		return nil, err
	}
	for _, pair := range [][2]string{{"service_tier", "service_tier"}, {"quality", "quality"}, {"size", "resolution"}, {"resolution", "resolution"}, {"n", "quantity"}, {"quantity", "quantity"}, {"seconds", "duration"}, {"duration", "duration"}, {"tool_category", "tool_category"}} {
		if value, ok := fields[pair[0]]; ok {
			attrs[pair[1]] = value
		}
	}
	for _, name := range []string{"quantity", "duration"} {
		if value, ok := attrs[name].(string); ok {
			parsed, err := decimal.NewFromString(value)
			if err != nil {
				return nil, fmt.Errorf("invalid billing quantity")
			}
			attrs[name] = json.Number(parsed.String())
		}
	}
	projectReasoningBillingAttributes(fields, attrs)
	if format == types.RelayFormatClaude {
		var request dto.ClaudeRequest
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, fmt.Errorf("invalid Claude billing request")
		}
		effort, present, err := request.ResolveEffectiveEffort()
		if err != nil {
			return nil, err
		}
		delete(attrs, "effort")
		if present {
			attrs["effort"] = strings.TrimSpace(effort)
		}
	}
	if effort, ok := attrs["effort"].(string); ok && effort != "" {
		effort = strings.ToLower(effort)
		attrs["effort"] = effort
		if _, exists := attrs["thinking"]; !exists {
			attrs["thinking"] = "on"
			if effort == "none" {
				attrs["thinking"] = "off"
			}
		}
	}
	if err := billingexpr.ValidateAttributes(attrs); err != nil {
		return nil, err
	}
	return attrs, nil
}

func projectReasoningBillingAttributes(fields, attrs map[string]interface{}) {
	copyFields := func(source map[string]interface{}, pairs ...string) {
		for i := 0; i < len(pairs); i += 2 {
			if value, present := source[pairs[i]]; present {
				attrs[pairs[i+1]] = value
			}
		}
	}
	thinkingBudget := func(value interface{}) {
		attrs["thinking_budget_tokens"] = value
		if raw, ok := value.(json.Number); ok {
			budget, err := decimal.NewFromString(raw.String())
			if err != nil {
				return
			}
			switch {
			case budget.Equal(decimal.NewFromInt(-1)):
				attrs["thinking"] = "adaptive"
				delete(attrs, "thinking_budget_tokens")
			case budget.IsZero():
				attrs["thinking"] = "off"
			case budget.IsPositive():
				attrs["thinking"] = "on"
			}
		}
	}
	copyFields(fields, "temperature", "temperature", "top_p", "top_p", "reasoning_effort", "effort")
	if reasoning, ok := fields["reasoning"].(map[string]interface{}); ok {
		copyFields(reasoning, "effort", "effort")
		if budget, exists := reasoning["max_tokens"]; exists {
			thinkingBudget(budget)
		}
		if enabled, ok := reasoning["enabled"].(bool); ok {
			attrs["thinking"] = "off"
			if enabled {
				attrs["thinking"] = "on"
			}
		}
	}
	if thinking, ok := fields["thinking"].(map[string]interface{}); ok {
		if budget, exists := thinking["budget_tokens"]; exists {
			thinkingBudget(budget)
		}
		if mode, ok := thinking["type"].(string); ok {
			switch mode {
			case "enabled":
				attrs["thinking"] = "on"
			case "disabled":
				attrs["thinking"] = "off"
			case "adaptive":
				attrs["thinking"] = "adaptive"
			}
		}
	}
	if output, ok := fields["output_config"].(map[string]interface{}); ok {
		copyFields(output, "effort", "effort")
	}
	if config, ok := fields["generationConfig"].(map[string]interface{}); ok {
		copyFields(config, "temperature", "temperature", "topP", "top_p")
		if thinking, ok := config["thinkingConfig"].(map[string]interface{}); ok {
			copyFields(thinking, "thinkingLevel", "effort")
			if budget, exists := thinking["thinkingBudget"]; exists {
				thinkingBudget(budget)
			}
		}
	}
}

func ResolveIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{FrozenAt: time.Now().UTC(), Attributes: map[string]interface{}{}}
	if info != nil && info.BillingRequestInput != nil {
		var err error
		input, err = billingexpr.CloneRequestInput(*info.BillingRequestInput)
		if err != nil {
			return input, err
		}
	}
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	if info != nil {
		for key, value := range info.RequestHeaders {
			if _, exists := input.Headers[key]; !exists {
				input.Headers[key] = value
			}
		}
	}
	if len(input.Body) == 0 && c != nil && c.Request != nil && isJSONContentType(c.Request.Header.Get("Content-Type")) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return input, err
		}
		input.Body, err = storage.Bytes()
		if err != nil {
			return input, err
		}
	}
	if len(input.Body) == 0 && info != nil && info.Request != nil {
		var err error
		input.Body, err = common.Marshal(info.Request)
		if err != nil {
			return input, err
		}
	}
	if input.Attributes == nil {
		var format types.RelayFormat
		if info != nil {
			format = info.RelayFormat
		}
		var err error
		input.Attributes, err = billingAttributesFromBody(input.Body, format)
		if err != nil {
			return input, err
		}
	}
	return input, nil
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{FrozenAt: time.Now().UTC(), Headers: map[string]string{}, Attributes: map[string]interface{}{}}
	for key, value := range headers {
		input.Headers[key] = value
	}
	if request == nil {
		return input, nil
	}
	body, err := common.Marshal(request)
	if err != nil {
		return input, err
	}
	input.Body = body
	format, _ := relaycommon.GuessRelayFormatFromRequest(request)
	input.Attributes, err = billingAttributesFromBody(body, format)
	return input, err
}

// Image pricing follows the upstream validated scalar projection, including
// multipart requests. File data is never needed to price an image count.
func ResolveImageBillingRequestInput(c *gin.Context, info *relaycommon.RelayInfo, input billingexpr.RequestInput) (billingexpr.RequestInput, error) {
	if input.ImageCount != nil {
		return input, nil
	}
	request, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return input, nil
	}
	count, err := request.ImageCount(false)
	if err != nil {
		return input, err
	}
	input.Body, err = imageBillingBody(request, count)
	input.ImageCount = &count
	return input, err
}

func isJSONContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json")
}

// FreezeOutboundBillingAttributes is called after the final wire body has been
// built and validated, before the quote is estimated and funds reserved.
func FreezeOutboundBillingAttributes(info *relaycommon.RelayInfo, body []byte) error {
	format := info.GetFinalRequestRelayFormat()
	attrs, err := billingAttributesFromBody(body, format)
	if err != nil {
		return err
	}
	required, err := PricingRequirementsFromBody(info, body)
	if err != nil {
		return err
	}
	info.BillingRequirements = &required
	info.BillingRequestInput = &billingexpr.RequestInput{Attributes: attrs, FrozenAt: time.Now().UTC()}
	if format == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatClaude {
		effort, _ := attrs["effort"].(string)
		info.SetReasoningEffort(effort)
	}
	return nil
}

func imageBillingBody(request *dto.ImageRequest, count int) ([]byte, error) {
	body := map[string]any{"model": request.Model, "n": count, "size": request.Size, "quality": request.Quality}
	if request.BillingParameters != nil {
		body["parameters"] = request.BillingParameters
	}
	return common.Marshal(body)
}
