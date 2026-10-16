package dto

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
)

// ModelMarket is a read-only view of the authenticated user's current supply.
// Prices are platform configuration at 1x, never claimed as provider list prices.
type ModelMarket struct {
	SchemaVersion      int                       `json:"schema_version"`
	Models             []MarketModel             `json:"models"`
	Channels           []MarketChannel           `json:"channels"`
	Offers             []MarketOffer             `json:"offers"`
	Telemetry          MarketTelemetry           `json:"telemetry"`
	SupportedEndpoints map[string]MarketEndpoint `json:"supported_endpoints"`
}

type MarketEndpoint struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}

type MarketModel struct {
	ID                     string                  `json:"id"`
	Description            string                  `json:"description"`
	Vendor                 string                  `json:"vendor"`
	Icon                   string                  `json:"icon"`
	Capabilities           *ModelCapabilities      `json:"capabilities"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	Baselines              []MarketPricing         `json:"baselines"`
}

type MarketPricing struct {
	UsageReference         *types.UsageReference   `json:"usage_reference,omitempty"`
	ID                     string                  `json:"id"`
	Version                string                  `json:"version"`
	Configured             bool                    `json:"configured"`
	Values                 map[string]any          `json:"values"`
	PluginKey              string                  `json:"plugin_key,omitempty"`
	PluginName             string                  `json:"plugin_name,omitempty"`
	UsageSchema            map[string]any          `json:"usage_schema,omitempty"`
	CacheWriteMode         string                  `json:"cache_write_mode"`
	BillingDetails         MarketBillingDetails    `json:"billing_details"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
}

type MarketBillingDetails struct {
	ImageCount        bool                `json:"image_count,omitempty"`
	RequestRules      []MarketPricingRule `json:"request_rules,omitempty"`
	AudioTextBranches bool                `json:"audio_text_branches,omitempty"`
}

type MarketPricingRule struct {
	Condition  string `json:"condition"`
	Multiplier string `json:"multiplier"`
}

type MarketChannel struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Ratio string `json:"ratio"`
}

type MarketOffer struct {
	ID                     string                  `json:"id"`
	ModelID                string                  `json:"model_id"`
	ChannelID              int                     `json:"channel_id"`
	PricingID              string                  `json:"pricing_id"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
}

type MarketTelemetry struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}
