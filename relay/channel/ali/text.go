package ali

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/samber/lo"
)

// https://help.aliyun.com/document_detail/613695.html?spm=a2c4g.239470.0.0.1adb778fAdzP9w#341800c0f8w0r

const EnableSearchModelSuffix = "-internet"

// requestOpenAI2Ali renders the DashScope-native thinking controls.
// resolvedEffort is RelayInfo.ReasoningEffort, the host's single resolved
// reasoning intent for this attempt; DashScope reads enable_thinking instead of
// the OpenAI reasoning_effort field, and Bailian rejects a disabling
// reasoning_effort rather than turning thinking off.
func requestOpenAI2Ali(request dto.GeneralOpenAIRequest, upstreamModelName string, resolvedEffort string) (*dto.GeneralOpenAIRequest, error) {
	modelName := upstreamModelName
	if modelName == "" {
		modelName = request.Model
	}
	if !dto.IsQwenThinkingBudgetModel(modelName) {
		request.ThinkingBudget = nil
	}

	if resolvedEffort == string(reasoning.EffortNone) {
		if explicit, present := explicitEnableThinking(request.EnableThinking); present {
			// Both controls are user-supplied and contradict each other; picking
			// one silently would send something the client did not ask for.
			if explicit {
				return nil, reasoning.AsClientError(fmt.Errorf(
					"reasoning_effort %q disables thinking, but the request sets enable_thinking to true", reasoning.EffortNone,
				))
			}
		} else {
			request.EnableThinking = json.RawMessage("false")
		}
		request.ReasoningEffort = ""
	}

	// DashScope rejects top_p at the 0 and 1 boundaries, so an explicit value is
	// clamped into the open interval. The clamp stays at two decimals because
	// some models on the platform reject a third decimal with
	// "top_p参数非法：限制小数点[2]位".
	//
	// A request that omits top_p is left untouched: injecting a value would
	// silently replace the model's own default with near-greedy decoding.
	if request.TopP != nil {
		if *request.TopP >= 1 {
			request.TopP = lo.ToPtr(0.99)
		} else if *request.TopP <= 0 {
			request.TopP = lo.ToPtr(0.01)
		}
	}
	return &request, nil
}

// explicitEnableThinking reports the client's own enable_thinking switch and
// whether the request set it at all, so a value the client chose is never
// overwritten.
func explicitEnableThinking(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case "false":
		return false, true
	case "true":
		return true, true
	default:
		// A non-boolean value is not a thinking switch the adaptor can reason
		// about; leave it as the client sent it.
		return false, false
	}
}
