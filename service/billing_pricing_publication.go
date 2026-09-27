package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

// Publication consumes one fixed catalog revision. New quotes see switched
// active pointers; existing requests retain their immutable price and quote.
func publishAutomaticPricingCatalog(ctx context.Context) (*AutomaticPricingSyncResult, error) {
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, err
	}
	result := &AutomaticPricingSyncResult{Unconfigured: []string{}, UnconfiguredDetails: []AutomaticPricingUnconfigured{}, Errors: []string{}}
	origins, err := model.ListBillingPriceOrigins(ctx)
	if err != nil {
		return result, err
	}
	byModel := make(map[string]model.BillingPriceOrigin, len(origins))
	for _, origin := range origins {
		if origin.ChannelID == 0 {
			byModel[origin.Model] = origin
		}
	}
	active, err := model.ListActiveBillingPrices(ctx)
	if err != nil {
		return result, err
	}
	activeHashes := make(map[string]string, len(active))
	for _, price := range active {
		if price.ScopeKind == "" || price.ScopeKind == billing.PriceScopeGlobal {
			activeHashes[price.Model] = price.Hash
		}
	}
	configured, err := model.ListAutomaticBillingModelNames(ctx)
	if err != nil {
		return result, err
	}
	targets := make(map[string]bool, len(catalog.names)+len(configured))
	for _, name := range catalog.names {
		targets[name] = true
	}
	for _, name := range configured {
		for _, candidate := range reasoning.BillingModelCandidates(name) {
			if len(candidate) <= 255 && reasoning.ValidateBillingModelName(candidate) == nil {
				targets[candidate] = true
				break
			}
		}
	}
	// Retired source ownership is still automatic. An absent replacement must
	// disable that exact historical identity rather than silently become manual.
	for _, origin := range origins {
		targets[origin.Model] = true
	}
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	publications := []model.AutomaticBillingPrice{}
	deactivations := []model.AutomaticBillingPriceDeactivation{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		current := byModel[name]
		if current.Origin == model.BillingPriceOriginManual {
			continue
		}
		match := catalog.resolve(name)
		validIdentity := len(name) <= 255 && reasoning.ValidateBillingModelName(name) == nil
		if match.entry.Source == "" || !validIdentity {
			result.addUnconfigured(name, match.reasons)
			if validIdentity {
				deactivations = append(deactivations, model.AutomaticBillingPriceDeactivation{Model: name, ExpectedVersionID: current.VersionID})
			}
			continue
		}
		entry := match.entry
		entry.Price.Model = name
		price, err := ValidateTaskPluginPricePublication(entry.Price, "")
		if err != nil {
			return result, fmt.Errorf("validate automatic price %s: %w", name, err)
		}
		sourceHash := catalog.hashes[string(entry.Source)]
		price.Origin, price.SourceModel, price.SourceHash = string(entry.Source), entry.SourceModel, sourceHash
		hash, err := billing.Hash(price)
		if err != nil {
			return result, err
		}
		if current.Origin == string(entry.Source) && current.SourceModel == entry.SourceModel && current.SourceHash == sourceHash && activeHashes[name] == hash {
			continue
		}
		publications = append(publications, model.AutomaticBillingPrice{Price: price, Source: string(entry.Source), SourceModel: entry.SourceModel, SourceHash: sourceHash})
	}
	if err := result.boundDiagnostics(); err != nil {
		return result, err
	}
	// Batches retain the existing source-before-price lock order and manual
	// ownership checks. Failed batches remain retryable without duplicate versions.
	for start := 0; start < len(deactivations); start += 100 {
		end := min(start+100, len(deactivations))
		count, err := model.DeactivateAutomaticBillingPrices(ctx, deactivations[start:end], catalog.hashes)
		if err != nil {
			return result, fmt.Errorf("deactivate unavailable automatic prices: %w", err)
		}
		result.Deactivated += count
	}
	for start := 0; start < len(publications); start += 100 {
		end := min(start+100, len(publications))
		count, err := model.PublishAutomaticBillingPrices(ctx, publications[start:end], catalog.hashes)
		if err != nil {
			return result, fmt.Errorf("publish automatic pricing batch: %w", err)
		}
		result.Published += count
	}
	return result, nil
}
