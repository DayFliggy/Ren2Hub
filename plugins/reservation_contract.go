package plugins

import (
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

// ValidateFactoryPriceReservation checks the reviewed factory's ability to
// supply pre-send estimates. Completion samples are not reservation estimates.
// This applies only to new factory drafts/publications, never accepted quotes.
func ValidateFactoryPriceReservation(key string, price billing.PriceVersion) error {
	if key != "vidu" || price.UsageSchema == nil {
		return nil
	}
	// Vidu explicitly provides credits only on completion. The fixed upstream
	// snapshot declares no trustworthy estimate or upper bound for a new task.
	required := price.UsageSchema.Fields["credits"].Required
	if price.Mode == billing.ModeExpression {
		used, err := billingexpr.UsedUsageFields(price.Expression, price.UsageSchema)
		if err != nil {
			return err
		}
		required = required || used["credits"]
	}
	for _, line := range price.Prices {
		required = required || line.Quantity == "credits"
	}
	if required {
		return fmt.Errorf("vidu credits has no reviewed reservation estimate; use an explicit calls/per_call price without usage_schema")
	}
	return nil
}
