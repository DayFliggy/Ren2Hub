package jsplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func decodeUsageJSON(data []byte) (any, error) {
	var value any
	if err := common.DecodeJsonNumbers(bytes.NewReader(data), &value); err != nil {
		return nil, fmt.Errorf("plugin completion usage requires valid source JSON")
	}
	return value, nil
}

func usageRequestValue(value any) (any, error) {
	// Preserve original scalar types. Schema fields are checked individually;
	// unused protocol settings are not monetary facts. A float must never be
	// re-encoded as json.Number and thereby pretend to recover source precision.
	switch value.(type) {
	case map[string]any, []any, float64:
		return value, nil
	}
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := common.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (a *TaskAdaptor) submitUsageContext(c *gin.Context, info *relaycommon.RelayInfo) (map[string]any, error) {
	ctx, err := a.submitContext(c, info)
	if err != nil {
		return nil, err
	}
	// Account preferences are protocol settings, not metering evidence.
	delete(ctx, "userSetting")
	if a.routeRequest != nil {
		request, err := usageRequestValue(a.routeRequest.RequestBody)
		if err != nil {
			return nil, err
		}
		ctx["requestBody"] = request
	}
	return ctx, nil
}

func exactQueryContext(ctx map[string]any, task *model.Task) (map[string]any, error) {
	result := make(map[string]any, len(ctx))
	for key, value := range ctx {
		result[key] = value
	}
	if task != nil {
		for key, raw := range map[string][]byte{"data": task.Data, "state": task.PrivateData.PluginState} {
			if len(raw) == 0 {
				continue
			}
			value, err := decodeUsageJSON(raw)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
	}
	return result, nil
}

// usageResultContext is the parsed result extractUsageOnComplete receives. Like
// upstream's TaskInfo it carries, as plugin_state, the state this parse
// returned: the hook's task.state is still the state from before this poll.
func usageResultContext(result *relaycommon.TaskInfo) (map[string]any, error) {
	view := map[string]any{"taskId": result.TaskID, "status": result.Status, "progress": result.Progress}
	if len(result.PluginState) == 0 {
		return view, nil
	}
	state, err := decodeUsageJSON(result.PluginState)
	if err != nil {
		return nil, err
	}
	view["plugin_state"] = state
	return view, nil
}

// A batch entry names its exact source location. Parsed protocol data is not
// accepted as billing evidence because JS may already have rounded numbers.
func usageAtPath(root any, path []any) (any, error) {
	if len(path) == 0 || len(path) > 32 {
		return nil, fmt.Errorf("batch completion usage source path is missing or invalid")
	}
	value := root
	for _, part := range path {
		switch container := value.(type) {
		case map[string]any:
			key, ok := part.(string)
			if !ok {
				return nil, fmt.Errorf("invalid completion usage object path")
			}
			value, ok = container[key]
			if !ok {
				return nil, fmt.Errorf("completion usage source is missing")
			}
		case []any:
			index, ok := part.(float64)
			if !ok || index < 0 || index >= float64(len(container)) || math.Trunc(index) != index {
				return nil, fmt.Errorf("invalid completion usage array path")
			}
			value = container[int(index)]
		default:
			return nil, fmt.Errorf("invalid completion usage source path")
		}
	}
	return value, nil
}

// isAutoDuration reports whether a request duration is exactly -1, the
// vendor's "the model picks the length", written as a number or numeric text.
func isAutoDuration(value any) bool {
	var text string
	switch number := value.(type) {
	case float64:
		return number == -1
	case int64:
		return number == -1
	case int:
		return number == -1
	case json.Number:
		text = string(number)
	case string:
		text = strings.TrimSpace(number)
	default:
		return false
	}
	parsed, err := decimal.NewFromString(text)
	return err == nil && parsed.Equal(decimal.NewFromInt(-1))
}

func normalizeLimitedUsage(value any, maximum int, integer bool) (any, error) {
	exact, err := pluginruntime.ExactUsageValue(value)
	if err != nil {
		return nil, err
	}
	return billingexpr.NormalizeUsageValue(billingexpr.UsageField{Type: "number", Maximum: strconv.Itoa(maximum), Integer: integer}, exact)
}

func normalizePluginUsage(value any, schema pluginruntime.UsageFieldSchema) (any, error) {
	if len(schema.Enum) > 0 {
		return billingexpr.NormalizeUsageValue(billingexpr.UsageField{Type: "string", Enum: schema.Enum}, value)
	}
	if schema.Type == "boolean" {
		return billingexpr.NormalizeUsageValue(billingexpr.UsageField{Type: "boolean"}, value)
	}
	limit := relaycommon.MaxTaskDurationSeconds
	if schema.Unit == "count" {
		limit = kitdto.MaxImageN
	} else if schema.Unit == "token" || schema.Unit == "credit" {
		limit = math.MaxInt32
	}
	return normalizeLimitedUsage(value, limit, schema.Unit == "count" || schema.Unit == "token")
}

func (a *TaskAdaptor) validatedCompletionUsageFacts(facts any, models ...string) (map[string]any, error) {
	if facts == nil {
		return nil, nil
	}
	values, ok := facts.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("plugin usage hook must return an object")
	}
	schema := a.plugin.Meta.UsageFactsForModels(models...)
	validated := make(map[string]any, len(values))
	for key, value := range values {
		field, declared := schema[key]
		if !declared {
			return nil, fmt.Errorf("plugin usage field has no declared unit")
		}
		normalized, err := normalizePluginUsage(value, field)
		if err != nil {
			return nil, fmt.Errorf("plugin usage field is invalid: %w", err)
		}
		validated[key] = normalized
	}
	return validated, nil
}
