package billing

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

// Evidence is frozen with the price, so a later catalog refresh cannot replace
// the facts explaining an amount. SourcePath is a JSON pointer into the snapshot.
type PriceFieldEvidence struct {
	Target     string `json:"target"`
	SourcePath string `json:"source_path"`
	RawValue   string `json:"raw_value"`
	SourceUnit string `json:"source_unit"`
	Conversion string `json:"conversion"`
}

type PriceProvenance struct {
	Kind          string               `json:"kind"`
	Provider      string               `json:"provider,omitempty"`
	SnapshotID    int64                `json:"snapshot_id,omitempty"`
	ParserVersion string               `json:"parser_version,omitempty"`
	Fields        []PriceFieldEvidence `json:"fields,omitempty"`
	RetailReason  string               `json:"retail_reason,omitempty"`
}

type PricingSupport struct {
	Dimensions   []string `json:"dimensions"`
	ServiceTiers []string `json:"service_tiers"`
}

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

func ClonePriceProvenance(value *PriceProvenance) *PriceProvenance {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Fields = append([]PriceFieldEvidence(nil), value.Fields...)
	return &copy
}

func ClonePricingSupport(value *PricingSupport) *PricingSupport {
	if value == nil {
		return nil
	}
	return &PricingSupport{Dimensions: append([]string(nil), value.Dimensions...), ServiceTiers: append([]string(nil), value.ServiceTiers...)}
}

func ValidatePriceProvenance(version PriceVersion) error {
	if p := version.Provenance; p != nil {
		if p.SnapshotID < 0 || len(p.Fields) > 256 || len(p.RetailReason) > 2048 || len(p.ParserVersion) > 64 || len(p.Provider) > 255 || strings.TrimSpace(p.Provider) != p.Provider {
			return fmt.Errorf("invalid price provenance")
		}
		switch p.Kind {
		case "source", "evidence":
			if len(p.Fields) == 0 {
				return fmt.Errorf("price source evidence is required")
			}
			for _, field := range p.Fields {
				if strings.TrimSpace(field.Target) == "" || strings.TrimSpace(field.SourcePath) == "" || strings.TrimSpace(field.RawValue) == "" || strings.TrimSpace(field.SourceUnit) == "" || strings.TrimSpace(field.Conversion) == "" || len(field.Target) > 128 || len(field.SourceUnit) > 64 || len(field.Conversion) > 128 || len(field.SourcePath) > 2048 || len(field.RawValue) > 2048 {
					return fmt.Errorf("incomplete price field evidence")
				}
			}
		case "retail":
			if strings.TrimSpace(p.RetailReason) == "" || p.SnapshotID != 0 || len(p.Fields) != 0 {
				return fmt.Errorf("retail price requires an explanation and no source snapshot")
			}
		default:
			return fmt.Errorf("unsupported price provenance kind")
		}
	}
	if s := version.Support; s != nil {
		if len(s.Dimensions) > 64 || len(s.ServiceTiers) == 0 || len(s.ServiceTiers) > 16 {
			return fmt.Errorf("invalid price support contract")
		}
		for _, values := range [][]string{s.Dimensions, s.ServiceTiers} {
			seen := make(map[string]bool)
			for _, value := range values {
				if value == "" || value != strings.TrimSpace(value) || len(value) > 128 || seen[value] {
					return fmt.Errorf("invalid or duplicate price support value")
				}
				seen[value] = true
			}
		}
		actual := priceDimensions(version)
		for _, dimension := range s.Dimensions {
			if !actual[dimension] && version.Mode != ModeFree {
				return fmt.Errorf("price support declares an unpriced dimension %q", dimension)
			}
		}
		for _, tier := range s.ServiceTiers {
			if tier != "default" && !billingexpr.UsedAttributes(version.Expression, version.UsageSchema)["service_tier"] {
				return fmt.Errorf("price service tier requires an explicit expression policy")
			}
		}
	}
	return nil
}

func priceDimensions(version PriceVersion) map[string]bool {
	result := make(map[string]bool)
	for _, price := range version.Prices {
		result[price.Quantity] = true
	}
	if version.Mode == ModeExpression {
		for name := range billingexpr.UsedVars(version.Expression) {
			if tokenDimension(name) {
				result[name] = true
			}
		}
		if billingexpr.UsedVars(version.Expression)["image_count"] {
			result["images"] = true
		}
		if version.UsageSchema != nil {
			if fields, err := billingexpr.UsedUsageFields(version.Expression, version.UsageSchema); err == nil {
				for name := range fields {
					if version.UsageSchema.Fields[name].Type == "number" {
						result[name] = true
					}
				}
			}
		}
	}
	return result
}

// A fixed request price covers the request's ordinary token modalities. It
// never grants a contract for separately billed tools or physical quantities.
func fixedRequestCoversTokens(version PriceVersion) bool {
	if version.Mode != ModeUnitPrice || version.UsageSchema != nil {
		return false
	}
	fixed := version.FlatFee != ""
	for _, price := range version.Prices {
		if price.Quantity == "calls" {
			fixed = true
		} else if !strings.HasPrefix(price.Quantity, "tool:") {
			return false
		}
	}
	return fixed
}

// ValidatePricingRequirements checks one complete candidate. It never merges
// dimensions or service policies from a different source or a live catalog.
func ValidatePricingRequirements(version PriceVersion, required PricingRequirements) error {
	if required.Provider != "" && (version.Provenance == nil || version.Provenance.Provider != required.Provider) {
		return &PricingError{Code: "ambiguous_price_identity", Dimension: "provider"}
	}
	tier := strings.TrimSpace(required.ServiceTier)
	if tier == "" || tier == "standard" {
		tier = "default"
	}
	dimensions, tiers := priceDimensions(version), map[string]bool{"default": true}
	if version.Support != nil {
		tiers = make(map[string]bool)
		supported := make(map[string]bool)
		for _, value := range version.Support.Dimensions {
			supported[value] = dimensions[value]
		}
		dimensions = supported
		for _, value := range version.Support.ServiceTiers {
			tiers[value] = true
		}
	}
	if !tiers[tier] {
		return &PricingError{Code: "unverified_service_tier", ServiceTier: tier}
	}
	if version.Mode == ModeFree {
		return nil
	}
	for _, dimension := range required.Dimensions {
		if !dimensions[dimension] && !(tokenDimension(dimension) && fixedRequestCoversTokens(version)) {
			return &PricingError{Code: "missing_price_dimension", Dimension: dimension}
		}
	}
	return nil
}
