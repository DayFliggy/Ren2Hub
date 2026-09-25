// exportseed converts the checked-in fixed upstream snapshot, never live prices.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	_ "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/shopspring/decimal"
	"os"
	"slices"
	"sort"
	"strings"
)

const upstreamSHA = "d04c118c8803f49e0c9bab74dcf5b5efeab9464a"

//go:embed upstream-rc40.json
var upstreamData []byte

type resolvedPrice struct {
	Completion      decimal.Decimal            `json:"completion"`
	Cache           decimal.Decimal            `json:"cache"`
	CacheWrite      decimal.Decimal            `json:"cache_write"`
	Image           decimal.Decimal            `json:"image"`
	Audio           decimal.Decimal            `json:"audio"`
	AudioCompletion decimal.Decimal            `json:"audio_completion"`
	GeminiAudio     decimal.Decimal            `json:"gemini_audio_usd"`
	Tools           map[string]decimal.Decimal `json:"tools"`
}
type upstreamSnapshot struct {
	Source      string                                `json:"source"`
	Defaults    map[string]map[string]decimal.Decimal `json:"defaults"`
	Expressions map[string]string                     `json:"expressions"`
	Resolved    map[string]resolvedPrice              `json:"resolved"`
}
type priceCatalog struct {
	Source       string                 `json:"source"`
	Prices       []billing.PriceVersion `json:"prices"`
	Unconfigured []string               `json:"unconfigured"`
	Reasons      map[string]string      `json:"unconfigured_reasons"`
}

func main() {
	output := flag.String("output", "", "output JSON file; stdout when omitted")
	flag.Parse()
	catalog, err := initialCatalog()
	if err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		panic(err)
	}
	data = append(data, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = os.WriteFile(*output, data, 0644)
	}
	if err != nil {
		panic(err)
	}
}
func initialCatalog() (priceCatalog, error) {
	var source upstreamSnapshot
	if err := json.Unmarshal(upstreamData, &source); err != nil {
		return priceCatalog{}, err
	}
	if source.Source != upstreamSHA {
		return priceCatalog{}, fmt.Errorf("unexpected upstream snapshot")
	}
	catalog := priceCatalog{Source: upstreamSHA, Prices: []billing.PriceVersion{}, Unconfigured: []string{}, Reasons: map[string]string{}}
	versions := map[string]billing.PriceVersion{}
	for name, ratio := range source.Defaults["ModelRatio"] {
		if strings.Contains(name, "*") {
			catalog.Reasons[name] = "通配符不是可发布的精确模型价格"
			continue
		}
		resolved, ok := source.Resolved[name]
		if !ok {
			return catalog, fmt.Errorf("missing resolved snapshot for %s", name)
		}
		input := ratio.Mul(decimal.NewFromInt(2))
		version := billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{}}
		if input.IsZero() {
			version.Mode = billing.ModeFree
		} else {
			prices := map[string]decimal.Decimal{
				"p": input, "c": input.Mul(resolved.Completion), "cr": input.Mul(resolved.Cache),
				"cc": input.Mul(resolved.CacheWrite), "cc1h": input.Mul(resolved.CacheWrite).Mul(decimal.RequireFromString("1.6")),
				"img": input.Mul(resolved.Image), "ai": input.Mul(resolved.Audio), "ao": input.Mul(resolved.Audio).Mul(resolved.AudioCompletion),
			}
			if resolved.GeminiAudio.IsPositive() {
				prices["ai"] = resolved.GeminiAudio
			}
			for quantity, price := range prices {
				version.Prices = append(version.Prices, billing.UnitPrice{Quantity: quantity, Unit: "per_million_tokens", Price: price.String()})
			}
		}
		versions[name] = version
	}
	for name, price := range source.Defaults["ModelPrice"] {
		if strings.Contains(name, "*") || strings.HasPrefix(name, "sora-") || strings.HasPrefix(name, "veo-") || name == "dall-e-3" || name == "gpt-4o-mini-tts" || strings.HasPrefix(name, "suno_") {
			delete(versions, name)
			catalog.Reasons[name] = "需要供应商时长、质量或插件完成用量合同，不能转换为固定每次费用"
			continue
		}
		version := billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: price.String()}}}
		if name == "imagen-3.0-generate-002" || name == "black-forest-labs/flux-1.1-pro" {
			version.Prices[0].Quantity, version.Prices[0].Unit = "images", "per_image"
		}
		if price.IsZero() {
			version.Mode, version.Prices = billing.ModeFree, []billing.UnitPrice{}
		}
		versions[name] = version
	}
	for name, expression := range source.Expressions {
		versions[name] = billing.PriceVersion{Model: name, Mode: billing.ModeExpression, Expression: "v1:" + expression, Prices: []billing.UnitPrice{}}
	}
	// The fixed factory snapshot declares capabilities and usage, not prices.
	// Never let a legacy per-call seed accidentally price a plugin task.
	for _, meta := range jsplugin.DefaultRegistry.Snapshot().Factory {
		for _, name := range meta.Models {
			delete(versions, name)
			catalog.Reasons[name] = "插件 " + meta.Key + " 的固定上游快照未提供可审阅的正式价格表达式"
		}
	}
	for name, version := range versions {
		if err := reasoning.ValidateBillingModelName(name); err != nil {
			catalog.Reasons[name] = "该名称不是本地可命中的规范价格键"
			continue
		}
		if version.Mode != billing.ModeFree {
			resolved, ok := source.Resolved[name]
			if !ok {
				return catalog, fmt.Errorf("missing tool snapshot for %s", name)
			}
			for tool, price := range resolved.Tools {
				version.Prices = append(version.Prices, billing.UnitPrice{Quantity: "tool:" + tool, Unit: "per_thousand_calls", Price: price.String()})
			}
		}
		sort.Slice(version.Prices, func(i, j int) bool { return version.Prices[i].Quantity < version.Prices[j].Quantity })
		prepared, err := billing.PreparePriceVersion(version)
		if err != nil {
			return catalog, fmt.Errorf("%s: %w", name, err)
		}
		catalog.Prices = append(catalog.Prices, prepared)
	}
	sort.Slice(catalog.Prices, func(i, j int) bool { return catalog.Prices[i].Model < catalog.Prices[j].Model })
	for name := range catalog.Reasons {
		catalog.Unconfigured = append(catalog.Unconfigured, name)
	}
	slices.Sort(catalog.Unconfigured)
	return catalog, nil
}
