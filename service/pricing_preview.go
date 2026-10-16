package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/google/uuid"
)

// EvaluatePricingPreview uses the production calculator without publishing a
// quote, usage evidence or wallet mutation. Callers resolve one price snapshot.
func EvaluatePricingPreview(name string, effective model.PricingValues, taskUsage bool, schema *billingexpr.UsageSchema, usage billing.Usage, input billingexpr.RequestInput, instant time.Time) (*billing.ChargeResult, error) {
	version, err := billing.NewModelPriceVersion(name, billing.PricingValues(effective), taskUsage)
	if err != nil {
		return nil, err
	}
	version.UsageSchema = schema
	quote, err := billing.NewQuote(version, "1", nil, instant)
	if err != nil {
		return nil, err
	}
	cipher, err := billing.NewEvidenceCipher([]byte(common.CryptoSecret))
	if err != nil {
		return nil, common.NewMessage("simulation encryption is not configured")
	}
	identity := uuid.NewString()
	_, charge, err := cipher.Evaluate(quote, usage, input, billing.EvidenceBinding{RequestID: identity, AttemptID: identity, ConfigHash: billing.PricingConfigHash(quote), Phase: "preview"})
	return charge, err
}
