package main

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/shopspring/decimal"
	"slices"
	"sort"
	"strings"
)

func main() {
	prices, unconfigured := InitialBillingPriceCatalog()
	data, err := common.Marshal(map[string]interface{}{"prices": prices, "unconfigured": unconfigured, "source": "checked-in-local-defaults"})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
}

func InitialBillingPriceCatalog() ([]billing.PriceVersion, []string) {
	versions := map[string]billing.PriceVersion{}
	var unconfigured []string
	for name, ratio := range ratio_setting.GetDefaultModelRatioMap() {
		if strings.Contains(name, "*") {
			unconfigured = append(unconfigured, name)
			continue
		}
		input := decimal.NewFromFloat(ratio).Mul(decimal.NewFromInt(2))
		version := billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{}}
		if input.IsZero() {
			version.Mode = billing.ModeFree
			versions[name] = version
			continue
		}
		cache, _ := ratio_setting.GetCacheRatio(name)
		write, _ := ratio_setting.GetCreateCacheRatio(name)
		image, _ := ratio_setting.GetImageRatio(name)
		prices := map[string]decimal.Decimal{
			"p": input, "c": input.Mul(decimal.NewFromFloat(ratio_setting.GetCompletionRatio(name))),
			"cr": input.Mul(decimal.NewFromFloat(cache)), "cc": input.Mul(decimal.NewFromFloat(write)), "cc1h": input.Mul(decimal.NewFromFloat(write)).Mul(decimal.RequireFromString("1.6")),
			"img": input.Mul(decimal.NewFromFloat(image)), "ai": input.Mul(decimal.NewFromFloat(ratio_setting.GetAudioRatio(name))),
			"ao": input.Mul(decimal.NewFromFloat(ratio_setting.GetAudioRatio(name))).Mul(decimal.NewFromFloat(ratio_setting.GetAudioCompletionRatio(name))),
		}
		if audio := operation_setting.GetGeminiInputAudioPricePerMillionTokens(name); audio > 0 {
			prices["ai"] = decimal.NewFromFloat(audio)
		}
		keys := make([]string, 0, len(prices))
		for key := range prices {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			version.Prices = append(version.Prices, billing.UnitPrice{Quantity: key, Unit: "per_million_tokens", Price: prices[key].String()})
		}
		versions[name] = version
	}
	for name, price := range ratio_setting.GetDefaultModelPriceMap() {
		if strings.Contains(name, "*") {
			unconfigured = append(unconfigured, name)
			continue
		}
		// These defaults depend on adapter-specific duration/quality rules;
		// a flat USD/call conversion would silently change their prices.
		if strings.HasPrefix(name, "sora-") || strings.HasPrefix(name, "veo-") || name == "dall-e-3" || name == "gpt-4o-mini-tts" {
			delete(versions, name)
			unconfigured = append(unconfigured, name)
			continue
		}
		version := billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: decimal.NewFromFloat(price).String()}}}
		if name == "imagen-3.0-generate-002" || name == "black-forest-labs/flux-1.1-pro" {
			version.Prices[0].Quantity = "images"
			version.Prices[0].Unit = "per_image"
		}
		if price == 0 {
			version.Mode = billing.ModeFree
			version.Prices = nil
		}
		versions[name] = version
	}
	keys := make([]string, 0, len(versions))
	for key := range versions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]billing.PriceVersion, 0, len(keys))
	for _, key := range keys {
		version := versions[key]
		if version.Mode != billing.ModeFree {
			for _, tool := range []string{"web_search", "web_search_preview", "file_search", "google_search", "image_generation"} {
				version.Prices = append(version.Prices, billing.UnitPrice{Quantity: "tool:" + tool, Unit: "per_thousand_calls", Price: decimal.NewFromFloat(operation_setting.GetToolPriceForModel(tool, key)).String()})
			}
		}
		result = append(result, version)
	}
	sort.Strings(unconfigured)
	return result, slices.Compact(unconfigured)
}
