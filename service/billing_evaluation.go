package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"math"
	"strconv"
)

// TaskExpressionUsage restores the upstream VM's numeric types at its boundary.
// The provider's decimal evidence remains intact in the separately stored usage.
func TaskExpressionUsage(facts map[string]any, schema *billingexpr.UsageSchema) (map[string]any, error) {
	values := make(map[string]any, len(facts))
	for name, value := range facts {
		if schema == nil {
			return nil, fmt.Errorf("task usage schema is missing")
		}
		field, ok := schema.Fields[name]
		if !ok {
			return nil, fmt.Errorf("task usage field is not declared")
		}
		if field.Type == "number" {
			if number, ok := value.(float64); ok {
				value = strconv.FormatFloat(number, 'g', -1, 64)
			}
		}
		normalized, err := billingexpr.NormalizeUsageValue(field, value)
		if err != nil {
			return nil, err
		}
		value = normalized
		if field.Type == "number" {
			number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
				return nil, fmt.Errorf("invalid task numeric usage")
			}
			values[name] = number
		} else {
			values[name] = value
		}
	}
	return values, nil
}

func evaluateRequestBilling(info *relaycommon.RelayInfo, usage billing.Usage) (billing.Usage, *billing.ChargeResult, error) {
	if session, ok := info.Billing.(*BillingSession); ok {
		session.mu.Lock()
		settling := session.settlementStarted || session.settled
		session.mu.Unlock()
		if settling && info.PriceData.Usage != nil && info.PriceData.Usage.ReadyForSettlement() && info.PriceData.Usage.Evidence != nil && info.PriceData.Charge != nil {
			return replayIdenticalBillingUsage(info.PriceData.Quote, *info.PriceData.Usage, usage, info.RequestId, info.BillingAttemptID)
		}
	}
	if info.BillingRequestInput == nil {
		return usage, nil, fmt.Errorf("billing request input is missing")
	}
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech, relayconstant.RelayModeAudioTranscription, relayconstant.RelayModeAudioTranslation:
		usage.PricingKind = "audio"
	}
	input, err := billingexpr.CloneRequestInput(*info.BillingRequestInput)
	if err != nil {
		return usage, nil, err
	}
	if raw, ok := usage.Quantities["images"]; ok {
		count, err := strconv.Atoi(raw)
		if err != nil {
			return usage, nil, fmt.Errorf("invalid completed image count")
		}
		input.ImageCount = &count
	}
	return model.EvaluateBilling(info.PriceData.Quote, usage, input, info.RequestId, info.BillingAttemptID, "settlement")
}

func publicBillingUsage(usage *billing.Usage) *billing.Usage {
	if usage == nil {
		return nil
	}
	projection := *usage
	projection.Evidence = nil
	projection.ActualFacts = nil
	projection.EstimatedFacts = nil
	return &projection
}

// Recovery repeats an existing evaluation. It must not create a new nonce or
// read the clock again for the same completed consumption.
func replayIdenticalBillingUsage(quote *billing.Quote, stored, observed billing.Usage, requestID, attemptID string) (billing.Usage, *billing.ChargeResult, error) {
	original := stored
	stored.Evidence, observed.Evidence = nil, nil
	storedHash, err := billing.Hash(stored)
	if err != nil {
		return original, nil, err
	}
	observedHash, err := billing.Hash(observed)
	if err != nil {
		return original, nil, err
	}
	if storedHash != observedHash {
		return original, nil, model.ErrBillingEventConflict
	}
	result, err := model.ReplayBilling(quote, original, requestID, attemptID, "settlement")
	return original, result, err
}
