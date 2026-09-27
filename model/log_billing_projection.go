package model

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
)

var quoteHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// User billing evidence is decoded through the frozen domain contract. Unknown
// nested metadata, provider request bodies, attributes and headers never pass.
func userBillingLogMetadata(raw string) map[string]any {
	var fields map[string]json.RawMessage
	result := map[string]any{}
	if common.UnmarshalJsonStr(raw, &fields) != nil {
		return result
	}
	var charge billing.ChargeResult
	if value := fields["charge_result"]; len(value) > 0 && string(value) != "null" && common.Unmarshal(value, &charge) == nil && charge.CalculationVersion == billing.CalculationVersion && charge.FinalQuota >= 0 {
		if charge.Trace != nil {
			rules := charge.Trace.RequestRules[:0]
			for _, rule := range charge.Trace.RequestRules {
				// Frozen v2 expressions never expose arbitrary request sources; discard
				// historical legacy-rule strings rather than rendering their payloads.
				if !strings.Contains(rule.Cond, "param(") && !strings.Contains(rule.Cond, "header(") && len(rule.Cond) <= 4096 {
					rules = append(rules, rule)
				}
			}
			charge.Trace.RequestRules = rules
		}
		result["charge_result"] = charge
	}
	var priceID int64
	if common.Unmarshal(fields["price_version_id"], &priceID) == nil && priceID > 0 {
		result["price_version_id"] = priceID
	}
	var scope string
	if common.Unmarshal(fields["price_scope"], &scope) == nil && (scope == billing.PriceScopeGlobal || scope == billing.PriceScopeChannel) {
		result["price_scope"] = scope
		var channelID int
		if common.Unmarshal(fields["price_channel_id"], &channelID) == nil && ((scope == billing.PriceScopeGlobal && channelID == 0) || (scope == billing.PriceScopeChannel && channelID > 0)) {
			result["price_channel_id"] = channelID
		}
	}
	for _, key := range []string{"quote_hash", "price_version_hash"} {
		var hash string
		if common.Unmarshal(fields[key], &hash) == nil && quoteHashPattern.MatchString(hash) {
			result[key] = hash
		}
	}
	var mode string
	if common.Unmarshal(fields["billing_mode"], &mode) == nil && (mode == billing.ModeFree || mode == billing.ModeUnitPrice || mode == billing.ModeExpression) {
		result["billing_mode"] = mode
	}
	for _, key := range []string{"upstream_model_name", "response_model_name"} {
		var name string
		if common.Unmarshal(fields[key], &name) == nil && len(name) <= 512 && !strings.ContainsAny(name, "\r\n\x00") {
			result[key] = name
		}
	}
	var usage billing.Usage
	if common.Unmarshal(fields["billing_usage"], &usage) == nil && len(fields["billing_usage"]) > 0 {
		usage.EstimatedFacts = nil
		usage.ActualFacts = nil
		usage.Quantities = nil
		if len(usage.Source) > 128 || strings.ContainsAny(usage.Source, "\r\n\x00") {
			usage.Source = ""
		}
		result["billing_usage"] = usage
	}
	return result
}
