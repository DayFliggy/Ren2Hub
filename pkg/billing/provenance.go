package billing

import (
	"fmt"
	"strings"
)

type PricingRequirements struct {
	Dimensions  []string
	ServiceTier string
	Provider    string
}

type PricingError struct {
	Code        string
	Dimension   string
	ServiceTier string
}

func (err *PricingError) ErrorCode() string { return err.Code }
func (err *PricingError) Error() string {
	if err.ServiceTier != "" {
		return fmt.Sprintf("%s: service tier %q", err.Code, err.ServiceTier)
	}
	return fmt.Sprintf("%s: dimension %q", err.Code, err.Dimension)
}

// Model prices cover ordinary token modalities using NewAPI normalization.
// Tools remain separate, explicitly configured surcharges. A service-tier
// allowlist must not override an administrator's expression conditions.
func ValidatePricingRequirements(version PriceVersion, required PricingRequirements) error {
	if err := validateModelPriceVersion(version); err != nil {
		return err
	}
	prices := make(map[string]bool, len(version.Prices))
	for _, price := range version.Prices {
		prices[price.Quantity] = true
	}
	for _, dimension := range required.Dimensions {
		if strings.HasPrefix(dimension, "tool:") {
			if !prices[dimension] {
				return &PricingError{Code: "missing_price_dimension", Dimension: dimension}
			}
			continue
		}
		if tokenDimension(dimension) || dimension == "calls" {
			continue
		}
		if dimension == "images" && (version.Mode == ModePerCall || version.Mode == ModeExpression && !version.TaskUsage) {
			continue
		}
		if version.TaskUsage && version.UsageSchema != nil {
			if _, exists := version.UsageSchema.Fields[dimension]; exists {
				continue
			}
		}
		return &PricingError{Code: "missing_price_dimension", Dimension: dimension}
	}
	return nil
}
