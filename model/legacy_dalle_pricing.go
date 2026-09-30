package model

// Deprecated model compatibility: OpenAI removed DALL·E 2 and DALL·E 3 on
// 2026-05-12. Keep conversion of existing prices available for compatible
// upstreams, deriving every multiplier from the legacy DTO billing logic.
// https://developers.openai.com/api/docs/deprecations#2025-11-14-dalle-model-snapshots

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/billing"
)

func legacyDallePricingRules(name string) []LegacyPricingRule {
	if !strings.HasPrefix(name, "dall-e") {
		return nil
	}
	var rules []LegacyPricingRule
	if name != "dall-e-3" {
		for _, size := range []string{"256x256", "512x512"} {
			rules = append(rules, LegacyPricingRule{`param("size") == "` + size + `"`, billing.LegacyImagePriceRatio(name, size, "")})
		}
	}
	rectangleRatio := billing.LegacyImagePriceRatio(name, "1024x1792", "")
	if name != "dall-e-2" && name != "dall-e" {
		rules = append(rules, LegacyPricingRule{`param("size") == "1024x1792" || param("size") == "1792x1024"`, rectangleRatio})
	}
	if name == "dall-e-3" {
		rules = append(rules,
			LegacyPricingRule{`param("quality") == "hd" && param("size") != "1024x1792" && param("size") != "1792x1024"`, billing.LegacyImagePriceRatio(name, "1024x1024", "hd")},
			LegacyPricingRule{`param("quality") == "hd" && (param("size") == "1024x1792" || param("size") == "1792x1024")`, billing.LegacyImagePriceRatio(name, "1024x1792", "hd") / rectangleRatio})
	}
	return rules
}
