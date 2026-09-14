package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

func ProjectVertexGeminiRequest(info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (*dto.GeminiChatRequest, error) {
	if !model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled {
		return request, nil
	}
	policy := types.ConversionLossPolicySafe
	if info != nil && info.ConversionPolicy != "" {
		policy = info.ConversionPolicy
	}
	if info != nil && info.ConversionPlan != nil {
		policy = info.ConversionPlan.Policy
	}
	result, err := relayconvert.ProjectGeminiToolHistoryWithoutIDs(request, policy)
	if info != nil && info.ConversionPlan != nil && result != nil {
		for i := range result.Diagnostics {
			result.Diagnostics[i].From = info.ConversionPlan.From
		}
	}
	RecordRequestConversionDiagnostics(info, result)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeminiChatRequest), nil
}
