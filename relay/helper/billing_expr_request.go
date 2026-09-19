package helper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// BillingAttributesFromBody projects only registered scalar attributes. Request
// bodies and headers never become expression inputs or persisted evidence.
func BillingAttributesFromBody(body []byte) (map[string]interface{}, error) {
	attrs := map[string]interface{}{}
	if len(body) == 0 {
		return attrs, nil
	}
	var fields map[string]interface{}
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	for _, pair := range [][2]string{{"service_tier", "service_tier"}, {"quality", "quality"}, {"size", "resolution"}, {"resolution", "resolution"}, {"n", "quantity"}, {"quantity", "quantity"}, {"seconds", "duration"}, {"duration", "duration"}, {"tool_category", "tool_category"}} {
		if value, ok := fields[pair[0]]; ok {
			attrs[pair[1]] = value
		}
	}
	for _, name := range []string{"quantity", "duration"} {
		if value, ok := attrs[name].(string); ok {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid billing quantity")
			}
			attrs[name] = parsed
		}
	}
	if err := billingexpr.ValidateAttributes(attrs); err != nil {
		return nil, err
	}
	return attrs, nil
}

func ResolveIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	if info != nil && info.BillingRequestInput != nil {
		return cloneRequestInput(*info.BillingRequestInput), nil
	}
	if info != nil && info.Request != nil {
		return BuildBillingExprRequestInputFromRequest(info.Request, nil)
	}
	input := billingexpr.RequestInput{FrozenAt: time.Now().UTC(), Attributes: map[string]interface{}{}}
	if c == nil || c.Request == nil || !isJSONContentType(c.Request.Header.Get("Content-Type")) {
		return input, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return input, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return input, err
	}
	input.Attributes, err = BillingAttributesFromBody(body)
	return input, err
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, _ map[string]string) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{FrozenAt: time.Now().UTC(), Attributes: map[string]interface{}{}}
	if request == nil {
		return input, nil
	}
	body, err := common.Marshal(request)
	if err != nil {
		return input, err
	}
	input.Attributes, err = BillingAttributesFromBody(body)
	return input, err
}

func cloneRequestInput(src billingexpr.RequestInput) billingexpr.RequestInput {
	input := billingexpr.RequestInput{FrozenAt: src.FrozenAt, Attributes: map[string]interface{}{}}
	for name, value := range src.Attributes {
		input.Attributes[name] = value
	}
	return input
}

func isJSONContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json")
}

// FreezeOutboundBillingAttributes is called after the final wire body has been
// built and validated, before the quote is estimated and funds reserved.
func FreezeOutboundBillingAttributes(info *relaycommon.RelayInfo, body []byte) error {
	attrs, err := BillingAttributesFromBody(body)
	if err != nil {
		return err
	}
	info.BillingRequestInput = &billingexpr.RequestInput{Attributes: attrs, FrozenAt: time.Now().UTC()}
	return nil
}
