package types

// UsageReference is a read-only, 1x estimate for a plugin's first declared
// example. It is not a configured unit price or a rounded wallet charge.
type UsageReference struct {
	Status      string         `json:"status"`
	AmountUSD   string         `json:"amount_usd,omitempty"`
	Label       string         `json:"label,omitempty"`
	Facts       map[string]any `json:"facts,omitempty"`
	PluginKey   string         `json:"plugin_key,omitempty"`
	PluginName  string         `json:"plugin_name,omitempty"`
	MatchedTier string         `json:"matched_tier,omitempty"`
	Reason      string         `json:"reason,omitempty"`
}
