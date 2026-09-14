package ollama

import (
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func validateOllamaRequest(request *dto.GeneralOpenAIRequest, policy types.ConversionLossPolicy) ([]types.ConversionDiagnostic, error) {
	requirements, err := relayconvert.AnalyzeRequest(request)
	if err != nil {
		return nil, err
	}
	return relaycapability.OllamaChatProjection().Check(requirements, policy)
}
