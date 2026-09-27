package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

// Each source owns one complete, validated snapshot. Resolution is read-only;
// publication, drafts and restore all use the same identity and rejection rules.
type automaticPricingSourceCatalog struct {
	source        autopricing.SourceID
	entries       map[string]autopricing.Entry
	rejections    map[string]string
	rejectedNames []string
	blocked       map[string]bool
}

type automaticPriceCatalog struct {
	sources []automaticPricingSourceCatalog
	hashes  map[string]string
	names   []string
}

type automaticPriceMatch struct {
	entry   autopricing.Entry
	reasons []AutomaticPricingRejection
	err     error
}

func loadAutomaticPriceCatalog(ctx context.Context) (*automaticPriceCatalog, error) {
	rows, err := model.ListBillingPricingSources(ctx)
	if err != nil {
		return nil, err
	}
	bySource := make(map[string]model.BillingPricingSource, len(rows))
	for _, row := range rows {
		bySource[row.Source] = row
	}
	catalog := &automaticPriceCatalog{hashes: make(map[string]string)}
	names := make(map[string]bool)
	for _, spec := range billingPricingSources {
		row := bySource[string(spec.Source)]
		catalog.hashes[string(spec.Source)] = row.Hash
		source := automaticPricingSourceCatalog{
			source: spec.Source, entries: map[string]autopricing.Entry{},
			rejections: map[string]string{}, blocked: map[string]bool{},
		}
		if row.Document != "" {
			if pricingDocumentHash([]byte(row.Document)) != row.Hash {
				source.rejections["*"] = "pricing cache integrity mismatch"
				catalog.sources = append(catalog.sources, source)
				continue
			}
			entries, rejected, err := autopricing.ParseWithReport(spec.Source, []byte(row.Document))
			if err != nil {
				source.rejections["*"] = fmt.Sprintf("pricing cache: %v", err)
				catalog.sources = append(catalog.sources, source)
				continue
			}
			for _, rejection := range rejected {
				source.rejections[rejection.Model] = rejection.Reason
			}
			for name, entry := range entries {
				entry.Price.Provenance.SnapshotID = row.SnapshotID
				entry.Price.Origin, entry.Price.SourceModel, entry.Price.SourceHash = string(spec.Source), entry.SourceModel, row.Hash
				validationErr := reasoning.ValidateBillingModelName(name)
				if validationErr == nil && (len(name) > 255 || len(entry.SourceModel) > 255) {
					validationErr = fmt.Errorf("model identity exceeds 255 bytes")
				}
				if validationErr == nil {
					entry.Price, validationErr = ValidateTaskPluginPricePublication(entry.Price, "")
				}
				if validationErr != nil {
					source.rejections[name] = validationErr.Error()
					continue
				}
				source.entries[name] = entry
				names[name] = true
			}
			for name := range source.rejections {
				source.blocked[name] = true
				source.rejectedNames = append(source.rejectedNames, name)
				names[name] = true
			}
			sort.Strings(source.rejectedNames)
		} else if row.LastError != "" {
			// A source without a validated snapshot is unavailable, but it must
			// not prevent another source from resolving the same model.
			source.rejections["*"] = fmt.Sprintf("%s has no validated pricing snapshot: %s", spec.Source, row.LastError)
		}
		catalog.sources = append(catalog.sources, source)
	}
	for name := range names {
		catalog.names = append(catalog.names, name)
	}
	if len(catalog.names) == 0 {
		return nil, fmt.Errorf("no validated pricing snapshot is available")
	}
	sort.Strings(catalog.names)
	return catalog, nil
}

func (catalog *automaticPriceCatalog) resolve(name string, requirements ...billing.PricingRequirements) automaticPriceMatch {
	required := billing.PricingRequirements{}
	if len(requirements) != 0 {
		required = requirements[0]
	}
	match := automaticPriceMatch{reasons: make([]AutomaticPricingRejection, len(catalog.sources))}
	blocked := make([]bool, len(catalog.sources))
	for i, source := range catalog.sources {
		match.reasons[i] = AutomaticPricingRejection{Source: string(source.source), Reason: "no complete price for this model or a supported alias"}
		if reason, exists := source.rejections["*"]; exists {
			match.reasons[i].Reason = reason
			blocked[i] = true
		}
	}
	for _, candidate := range reasoning.BillingModelCandidates(name) {
		// Exact identity in either source precedes a less-specific alias.
		for i, source := range catalog.sources {
			if blocked[i] {
				continue
			}
			if entry, exists := source.entries[candidate]; exists {
				if err := billing.ValidatePricingRequirements(entry.Price, required); err != nil {
					billing.RecordCandidateRejection(string(source.source))
					blocked[i], match.reasons[i].Reason = true, err.Error()
					match.err = err
					continue
				}
				match.entry = entry
				return match
			}
		}
		for i, source := range catalog.sources {
			if blocked[i] {
				continue
			}
			if autopricing.IsBlocked(source.blocked, candidate) {
				billing.RecordCandidateRejection(string(source.source))
				blocked[i] = true
				if reason, exists := source.rejections[candidate]; exists {
					match.reasons[i].Reason = reason
				} else {
					for _, rejected := range source.rejectedNames {
						if autopricing.IsBlocked(map[string]bool{rejected: true}, candidate) {
							match.reasons[i].Reason = source.rejections[rejected]
							break
						}
					}
				}
				continue
			}
			entry, exists := autopricing.Resolve(source.entries, candidate)
			if !exists {
				continue
			}
			entry.Price.Model = candidate
			price, err := ValidateTaskPluginPricePublication(entry.Price, "")
			if err != nil {
				blocked[i], match.reasons[i].Reason = true, err.Error()
				continue
			}
			entry.Price = price
			if err := billing.ValidatePricingRequirements(entry.Price, required); err != nil {
				billing.RecordCandidateRejection(string(source.source))
				blocked[i], match.reasons[i].Reason = true, err.Error()
				match.err = err
				continue
			}
			match.entry = entry
			return match
		}
	}
	return match
}

func AutomaticPricingDrafts(ctx context.Context) ([]billing.PriceVersion, []string, string, error) {
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	prices, unavailable := []billing.PriceVersion{}, []string{}
	for _, name := range catalog.names {
		if len(name) > 255 || reasoning.ValidateBillingModelName(name) != nil {
			unavailable = append(unavailable, name)
			continue
		}
		match := catalog.resolve(name)
		if match.entry.Source == "" {
			unavailable = append(unavailable, name)
			continue
		}
		price := match.entry.Price
		price.Model = name
		price, err = ValidateTaskPluginPricePublication(price, "")
		if err != nil {
			unavailable = append(unavailable, name)
			continue
		}
		prices = append(prices, price)
	}
	revisions := make([]string, 0, len(catalog.sources))
	for _, source := range catalog.sources {
		if hash := catalog.hashes[string(source.source)]; hash != "" {
			revisions = append(revisions, string(source.source)+":"+hash)
		}
	}
	return prices, unavailable, strings.Join(revisions, ";"), nil
}

func RestoreAutomaticPrice(ctx context.Context, name string, expectedVersion int64) (*billing.PriceVersion, error) {
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, err
	}
	match := catalog.resolve(name)
	if match.entry.Source == "" {
		return nil, fmt.Errorf("automatic price is unavailable or ambiguous")
	}
	entry := match.entry
	entry.Price.Model = name
	price, err := ValidateTaskPluginPricePublication(entry.Price, "")
	if err != nil {
		return nil, err
	}
	return model.RestoreAutomaticBillingPrice(ctx, price, string(entry.Source), entry.SourceModel, catalog.hashes[string(entry.Source)], expectedVersion, catalog.hashes)
}

// ResolveAndPublishAutomaticPrice may switch an automatically managed global
// identity to a complete alternative source. The observed version is the CAS
// fence, so a concurrent manual decision always wins.
func ResolveAndPublishAutomaticPrice(ctx context.Context, name string, required billing.PricingRequirements) (*billing.PriceVersion, error) {
	current, err := model.GetActiveBillingPrice(ctx, name)
	if err != nil {
		return nil, err
	}
	if current.ScopeKind != billing.PriceScopeGlobal || current.ChannelID != 0 || (current.Origin != model.BillingPriceOriginSub2API && current.Origin != model.BillingPriceOriginAxonHub) {
		return nil, fmt.Errorf("price is not an automatically managed global identity")
	}
	if err := billing.ValidatePricingRequirements(*current, required); err == nil {
		return current, nil
	}
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, err
	}
	match := catalog.resolve(name, required)
	if match.entry.Source == "" {
		parts := make([]string, 0, len(match.reasons))
		for _, reason := range match.reasons {
			parts = append(parts, reason.Source+": "+reason.Reason)
		}
		if match.err == nil {
			match.err = &billing.PricingError{Code: "missing_usage_contract"}
		}
		return nil, fmt.Errorf("no complete automatic price (%s): %w", strings.Join(parts, "; "), match.err)
	}
	entry := match.entry
	entry.Price.Model = name
	return model.RestoreAutomaticBillingPrice(ctx, entry.Price, string(entry.Source), entry.SourceModel, catalog.hashes[string(entry.Source)], current.ID, catalog.hashes)
}

func (result *AutomaticPricingSyncResult) addUnconfigured(name string, reasons []AutomaticPricingRejection) {
	result.UnconfiguredCount++
	if len(result.UnconfiguredDetails) >= 100 {
		return
	}
	detail := AutomaticPricingUnconfigured{Model: boundedPricingDiagnostic(name, 255), Reasons: make([]AutomaticPricingRejection, len(reasons))}
	for i, reason := range reasons {
		detail.Reasons[i] = AutomaticPricingRejection{Source: reason.Source, Reason: boundedPricingDiagnostic(reason.Reason, 128)}
	}
	result.Unconfigured = append(result.Unconfigured, detail.Model)
	result.UnconfiguredDetails = append(result.UnconfiguredDetails, detail)
}

// Task results use portable TEXT storage, including escaped source diagnostics.
func (result *AutomaticPricingSyncResult) boundDiagnostics() error {
	for {
		encoded, err := common.Marshal(result)
		if err != nil {
			return err
		}
		if len(encoded) <= 60<<10 {
			return nil
		}
		if len(result.UnconfiguredDetails) == 0 {
			return fmt.Errorf("automatic pricing diagnostics exceed durable result limit")
		}
		result.Unconfigured = result.Unconfigured[:len(result.Unconfigured)-1]
		result.UnconfiguredDetails = result.UnconfiguredDetails[:len(result.UnconfiguredDetails)-1]
	}
}

func boundedPricingDiagnostic(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit-3]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "..."
}
