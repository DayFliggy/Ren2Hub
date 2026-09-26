package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autopricing"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

const maxPricingDocumentBytes = 16 << 20

//go:embed pricingdata/*.json.gz
var bundledPricingSources embed.FS

type billingPricingSourceSpec struct {
	Source  autopricing.SourceID
	URL     string
	HashURL string
	Role    string
}

// Source order is the billing policy. A complete model definition comes from
// one source; mixing providers' individual dimensions could underprice a model.
var billingPricingSources = []billingPricingSourceSpec{
	{autopricing.SourceSub2API, "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json", "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.sha256", "primary"},
	{autopricing.SourceAxonHub, "https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json", "", "primary"},
	{autopricing.SourceModelsDev, "https://models.dev/api.json", "", "fallback"},
	{autopricing.SourceOpenRouter, "https://openrouter.ai/api/v1/models", "", "fallback"},
}

type AutomaticPricingSourceStatus struct {
	Source        string `json:"source"`
	URL           string `json:"url"`
	Role          string `json:"role"`
	Hash          string `json:"hash"`
	CheckedAt     int64  `json:"checked_at"`
	UpdatedAt     int64  `json:"updated_at"`
	LastError     string `json:"last_error"`
	ModelCount    int    `json:"model_count"`
	RejectedCount int    `json:"rejected_count"`
}

type AutomaticPricingStatus struct {
	Config  *model.BillingPricingConfig    `json:"config"`
	Sources []AutomaticPricingSourceStatus `json:"sources"`
	Origins []model.BillingPriceOrigin     `json:"origins"`
}

type AutomaticPricingSyncResult struct {
	Checked             int                            `json:"checked"`
	Changed             int                            `json:"changed"`
	Published           int                            `json:"published"`
	Deactivated         int                            `json:"deactivated"`
	Unconfigured        []string                       `json:"unconfigured"`
	UnconfiguredCount   int                            `json:"unconfigured_count"`
	Errors              []string                       `json:"errors"`
	UnconfiguredDetails []AutomaticPricingUnconfigured `json:"unconfigured_details"`
}

type AutomaticPricingRejection struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type AutomaticPricingUnconfigured struct {
	Model   string                      `json:"model"`
	Reasons []AutomaticPricingRejection `json:"reasons"`
}

type automaticPriceCatalog struct {
	entries    map[string]autopricing.Entry
	hashes     map[string]string
	sources    map[autopricing.SourceID]map[string]autopricing.Entry
	rejections map[autopricing.SourceID]map[string]string
	blocked    map[autopricing.SourceID]map[string]bool
	pending    map[string]bool
}

// InitializeAutomaticPricing supplies an offline first boot, then the system
// task performs remote checks. Never overwrite a persisted last-good snapshot.
func InitializeAutomaticPricing(ctx context.Context) error {
	for _, source := range billingPricingSources {
		if source.Role != "primary" {
			if err := model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{Source: string(source.Source)}); err != nil {
				return err
			}
			continue
		}
		compressed, err := bundledPricingSources.ReadFile("pricingdata/" + string(source.Source) + ".json.gz")
		if err != nil {
			return err
		}
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxPricingDocumentBytes+1))
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if len(data) > maxPricingDocumentBytes {
			return fmt.Errorf("bundled pricing source exceeds limit")
		}
		entries, err := autopricing.Parse(source.Source, data)
		if err != nil {
			return fmt.Errorf("bundled %s: %w", source.Source, err)
		}
		row := model.BillingPricingSource{Source: string(source.Source), Document: model.LongText(data), Hash: pricingDocumentHash(data), ModelCount: len(entries)}
		if err := model.EnsureBillingPricingSource(ctx, &row); err != nil {
			return err
		}
	}
	config, err := model.GetBillingPricingConfig(ctx)
	if err != nil || !config.Enabled {
		return err
	}
	_, err = publishAutomaticPricingCatalog(ctx)
	return err
}

func pricingDocumentHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func loadAutomaticPriceCatalog(ctx context.Context) (*automaticPriceCatalog, error) {
	rows, err := model.ListBillingPricingSources(ctx)
	if err != nil {
		return nil, err
	}
	bySource := make(map[string]model.BillingPricingSource, len(rows))
	for _, row := range rows {
		bySource[row.Source] = row
	}
	catalog := &automaticPriceCatalog{entries: map[string]autopricing.Entry{}, hashes: map[string]string{}, sources: map[autopricing.SourceID]map[string]autopricing.Entry{}, rejections: map[autopricing.SourceID]map[string]string{}, blocked: map[autopricing.SourceID]map[string]bool{}, pending: map[string]bool{}}
	for _, spec := range billingPricingSources {
		catalog.rejections[spec.Source] = map[string]string{}
		catalog.blocked[spec.Source] = map[string]bool{}
		row := bySource[string(spec.Source)]
		catalog.hashes[string(spec.Source)] = row.Hash
		if row.Document == "" {
			continue
		}
		if pricingDocumentHash([]byte(row.Document)) != row.Hash {
			return nil, fmt.Errorf("%s pricing cache integrity mismatch", spec.Source)
		}
		entries, rejected, err := autopricing.ParseWithReport(spec.Source, []byte(row.Document))
		if err != nil {
			return nil, fmt.Errorf("%s pricing cache: %w", spec.Source, err)
		}
		for _, rejection := range rejected {
			catalog.pending[rejection.Model] = true
			catalog.rejections[spec.Source][rejection.Model] = rejection.Reason
			catalog.blocked[spec.Source][rejection.Model] = true
		}
		for name, entry := range entries {
			validationErr := reasoning.ValidateBillingModelName(name)
			if validationErr == nil && (len(name) > 255 || len(entry.SourceModel) > 255) {
				validationErr = fmt.Errorf("model identity exceeds 255 bytes")
			}
			if validationErr == nil {
				entry.Price, validationErr = ValidateTaskPluginPricePublication(entry.Price, "")
			}
			if validationErr != nil {
				catalog.pending[name] = true
				catalog.rejections[spec.Source][name] = validationErr.Error()
				catalog.blocked[spec.Source][name] = true
				delete(entries, name)
				continue
			}
			entries[name] = entry
			if _, exists := catalog.entries[name]; !exists {
				catalog.entries[name] = entry
			}
		}
		catalog.sources[spec.Source] = entries
	}
	for name := range catalog.entries {
		delete(catalog.pending, name)
	}
	return catalog, nil
}

func (catalog *automaticPriceCatalog) resolve(name string) (autopricing.Entry, bool) {
	rejectedSources := map[autopricing.SourceID]bool{}
	for _, candidate := range reasoning.BillingModelCandidates(name) {
		// Exact entries outrank aliases, but a rejected more-specific request
		// identity cannot fall back to a simpler reasoning name in that source.
		for _, spec := range billingPricingSources {
			if rejectedSources[spec.Source] {
				continue
			}
			if entry, exists := catalog.sources[spec.Source][candidate]; exists {
				return entry, true
			}
		}
		for _, spec := range billingPricingSources {
			// A rejected concrete identity must not reuse a base price in the
			// same source. Rejection never suppresses another source.
			if rejectedSources[spec.Source] {
				continue
			}
			if autopricing.IsBlocked(catalog.blocked[spec.Source], candidate) {
				rejectedSources[spec.Source] = true
				continue
			}
			if entry, ok := autopricing.Resolve(catalog.sources[spec.Source], candidate); ok {
				entry.Price.Model = candidate
				price, err := ValidateTaskPluginPricePublication(entry.Price, "")
				if err != nil {
					catalog.rejections[spec.Source][candidate] = err.Error()
					rejectedSources[spec.Source] = true
					continue
				}
				entry.Price = price
				return entry, true
			}
		}
	}
	return autopricing.Entry{}, false
}

// Keep durable task history below MySQL TEXT's byte limit, even with long
// upstream names and JSON escaping. The full count remains available.
func (catalog *automaticPriceCatalog) describeUnconfigured(result *AutomaticPricingSyncResult) error {
	names := make([]string, 0, len(catalog.pending))
	for name := range catalog.pending {
		names = append(names, name)
	}
	sort.Strings(names)
	result.UnconfiguredCount = len(names)
	result.Unconfigured = []string{}
	result.UnconfiguredDetails = []AutomaticPricingUnconfigured{}
	for _, name := range names[:min(len(names), 100)] {
		detail := AutomaticPricingUnconfigured{Model: boundedPricingDiagnostic(name, 255), Reasons: []AutomaticPricingRejection{}}
		for _, spec := range billingPricingSources {
			reason := catalog.rejections[spec.Source][name]
			if reason == "" {
				// Reuse the exact alias rules for diagnostics as for selection.
				identities := make([]string, 0, len(catalog.rejections[spec.Source]))
				for identity := range catalog.rejections[spec.Source] {
					identities = append(identities, identity)
				}
				sort.Strings(identities)
				for _, identity := range identities {
					if autopricing.IsBlocked(map[string]bool{identity: true}, name) {
						reason = catalog.rejections[spec.Source][identity]
						break
					}
				}
			}
			if reason == "" {
				reason = "no complete price for this model or a supported alias"
			}
			detail.Reasons = append(detail.Reasons, AutomaticPricingRejection{Source: string(spec.Source), Reason: boundedPricingDiagnostic(reason, 128)})
		}
		result.Unconfigured = append(result.Unconfigured, detail.Model)
		result.UnconfiguredDetails = append(result.UnconfiguredDetails, detail)
	}
	return result.boundDiagnostics()
}

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

func GetAutomaticPricingStatus(ctx context.Context) (*AutomaticPricingStatus, error) {
	config, err := model.GetBillingPricingConfig(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := model.ListBillingPricingSources(ctx)
	if err != nil {
		return nil, err
	}
	origins, err := model.ListBillingPriceOrigins(ctx)
	if err != nil {
		return nil, err
	}
	result := &AutomaticPricingStatus{Config: config, Sources: []AutomaticPricingSourceStatus{}, Origins: origins}
	bySource := map[string]model.BillingPricingSource{}
	for _, row := range rows {
		bySource[row.Source] = row
	}
	for _, spec := range billingPricingSources {
		row := bySource[string(spec.Source)]
		status := AutomaticPricingSourceStatus{Source: string(spec.Source), URL: spec.URL, Role: spec.Role, Hash: row.Hash, CheckedAt: row.CheckedAt, UpdatedAt: row.UpdatedAt, LastError: row.LastError, ModelCount: row.ModelCount}
		if row.Document != "" {
			_, rejected, parseErr := autopricing.ParseWithReport(spec.Source, []byte(row.Document))
			status.RejectedCount = len(rejected)
			if parseErr != nil {
				status.LastError = "cached pricing document is invalid"
			}
			if pricingDocumentHash([]byte(row.Document)) != row.Hash {
				status.LastError = "cached pricing document integrity mismatch"
			}
		}
		result.Sources = append(result.Sources, status)
	}
	return result, nil
}

func pricingSourceRequest(ctx context.Context, client *http.Client, address string, previous *model.BillingPricingSource, limit int64) ([]byte, http.Header, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, false, err
	}
	request.Header.Set("Accept", "application/json, text/plain")
	if previous != nil && previous.Document != "" {
		if previous.ETag != "" {
			request.Header.Set("If-None-Match", previous.ETag)
		}
		if previous.LastModified != "" {
			request.Header.Set("If-Modified-Since", previous.LastModified)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && previous != nil && previous.Document != "" {
		return nil, response.Header, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, false, fmt.Errorf("source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, false, err
	}
	if int64(len(data)) > limit {
		return nil, nil, false, fmt.Errorf("pricing response exceeds %d bytes", limit)
	}
	return data, response.Header, false, nil
}

// fetchPricingSource uses an upstream digest when available and conditional
// requests otherwise. Only validated bytes become the new synchronization anchor.
func fetchPricingSource(ctx context.Context, client *http.Client, spec billingPricingSourceSpec, previous model.BillingPricingSource) (model.BillingPricingSource, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	validCache := previous.Document != "" && pricingDocumentHash([]byte(previous.Document)) == previous.Hash
	remoteHash := ""
	if spec.HashURL != "" {
		hashCtx, hashCancel := context.WithTimeout(ctx, 10*time.Second)
		data, _, _, err := pricingSourceRequest(hashCtx, client, spec.HashURL, nil, 1024)
		hashCancel()
		if err != nil {
			return previous, false, err
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 {
			return previous, false, fmt.Errorf("source SHA-256 is empty")
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return previous, false, fmt.Errorf("source SHA-256 is invalid")
		}
		remoteHash = strings.ToLower(fields[0])
		if validCache && remoteHash == previous.Hash {
			return previous, false, nil
		}
	}
	var conditional *model.BillingPricingSource
	if validCache && remoteHash == "" {
		conditional = &previous
	}
	data, headers, unchanged, err := pricingSourceRequest(ctx, client, spec.URL, conditional, maxPricingDocumentBytes)
	if err != nil {
		return previous, false, err
	}
	if unchanged {
		return previous, false, nil
	}
	hash := pricingDocumentHash(data)
	if remoteHash != "" && remoteHash != hash {
		return previous, false, fmt.Errorf("source SHA-256 does not match downloaded pricing")
	}
	entries, err := autopricing.Parse(spec.Source, data)
	if err != nil {
		return previous, false, err
	}
	updated := previous
	updated.Source, updated.Document, updated.Hash = string(spec.Source), model.LongText(data), hash
	updated.ETag, updated.LastModified = headers.Get("ETag"), headers.Get("Last-Modified")
	updated.ModelCount = len(entries)
	changed := !validCache || hash != previous.Hash
	if changed {
		updated.UpdatedAt = time.Now().Unix()
	}
	return updated, changed, nil
}

func SyncAutomaticPricing(ctx context.Context) (*AutomaticPricingSyncResult, error) {
	client := newProtectedFetchHTTPClient()
	return syncAutomaticPricing(ctx, client, billingPricingSources)
}

func syncAutomaticPricing(ctx context.Context, client *http.Client, sources []billingPricingSourceSpec) (*AutomaticPricingSyncResult, error) {
	result := &AutomaticPricingSyncResult{Unconfigured: []string{}, UnconfiguredDetails: []AutomaticPricingUnconfigured{}, Errors: []string{}}
	for _, spec := range billingPricingSources {
		if err := model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{Source: string(spec.Source)}); err != nil {
			return result, err
		}
	}
	rows, err := model.ListBillingPricingSources(ctx)
	if err != nil {
		return result, err
	}
	previous := map[string]model.BillingPricingSource{}
	for _, row := range rows {
		previous[row.Source] = row
	}
	var failures []error
	for _, spec := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		old := previous[string(spec.Source)]
		old.Source = string(spec.Source)
		row, changed, fetchErr := fetchPricingSource(ctx, client, spec, old)
		row.CheckedAt = time.Now().Unix()
		row.LastError = ""
		result.Checked++
		if fetchErr != nil {
			row.LastError = boundedPricingDiagnostic(fetchErr.Error(), 512)
			message := fmt.Sprintf("%s: %s", spec.Source, row.LastError)
			result.Errors = append(result.Errors, message)
			failures = append(failures, errors.New(message))
		}
		if err := model.SaveBillingPricingSourceCAS(ctx, &row, old.Hash); err != nil {
			return result, err
		}
		if changed {
			result.Changed++
		}
	}
	publication, err := publishAutomaticPricingCatalog(ctx)
	if publication != nil {
		result.Published, result.Unconfigured = publication.Published, publication.Unconfigured
		result.Deactivated = publication.Deactivated
		result.UnconfiguredCount = publication.UnconfiguredCount
		result.UnconfiguredDetails = publication.UnconfiguredDetails
	}
	if err != nil {
		failures = append(failures, err)
	}
	if err := result.boundDiagnostics(); err != nil {
		failures = append(failures, err)
	}
	return result, errors.Join(failures...)
}

func publishAutomaticPricingCatalog(ctx context.Context) (*AutomaticPricingSyncResult, error) {
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, err
	}
	result := &AutomaticPricingSyncResult{Unconfigured: []string{}, Errors: []string{}}
	origins, err := model.ListBillingPriceOrigins(ctx)
	if err != nil {
		return result, err
	}
	byModel := make(map[string]model.BillingPriceOrigin, len(origins))
	for _, origin := range origins {
		byModel[origin.Model] = origin
	}
	active, err := model.ListActiveBillingPrices(ctx)
	if err != nil {
		return result, err
	}
	activeHashes := make(map[string]string, len(active))
	for _, price := range active {
		activeHashes[price.Model] = price.Hash
	}
	// Publish known concrete aliases from channels as well as source identities.
	// Resolution happens here, never during request settlement.
	configured, err := model.ListAutomaticBillingModelNames(ctx)
	if err != nil {
		return result, err
	}
	// Persist unavailable concrete identities too: otherwise quote lookup could
	// skip a rejected reasoning identity and reuse an ordinary base price.
	requested := make(map[string]bool, len(configured)+len(catalog.pending))
	for _, name := range configured {
		requested[name] = true
	}
	for name := range catalog.pending {
		requested[name] = true
	}
	aliases := map[string]autopricing.Entry{}
	for name := range requested {
		identity := ""
		for _, candidate := range reasoning.BillingModelCandidates(name) {
			if len(candidate) <= 255 && reasoning.ValidateBillingModelName(candidate) == nil {
				identity = candidate
				break
			}
		}
		if identity == "" {
			continue
		}
		if byModel[identity].Origin == model.BillingPriceOriginManual {
			delete(catalog.pending, name)
			delete(catalog.pending, identity)
			continue
		}
		if _, exists := catalog.entries[identity]; exists {
			delete(catalog.pending, name)
			continue
		}
		if entry, ok := catalog.resolve(name); ok {
			entry.Price.Model = identity
			aliases[identity] = entry
			delete(catalog.pending, name)
		} else {
			delete(catalog.pending, name)
			catalog.pending[identity] = true
		}
	}
	for name, entry := range aliases {
		catalog.entries[name] = entry
		delete(catalog.pending, name)
	}
	for _, current := range origins {
		if current.Origin == model.BillingPriceOriginManual {
			delete(catalog.pending, current.Model)
			continue
		}
		if _, available := catalog.entries[current.Model]; !available {
			catalog.pending[current.Model] = true
		}
	}
	deactivations := []model.AutomaticBillingPriceDeactivation{}
	for name := range catalog.pending {
		if len(name) > 255 || reasoning.ValidateBillingModelName(name) != nil || byModel[name].Origin == model.BillingPriceOriginManual {
			continue
		}
		deactivations = append(deactivations, model.AutomaticBillingPriceDeactivation{Model: name, ExpectedVersionID: byModel[name].VersionID})
	}
	sort.Slice(deactivations, func(i, j int) bool { return deactivations[i].Model < deactivations[j].Model })
	names := make([]string, 0, len(catalog.entries))
	for name := range catalog.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	publications := make([]model.AutomaticBillingPrice, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entry := catalog.entries[name]
		current := byModel[name]
		if current.Origin == model.BillingPriceOriginManual {
			continue
		}
		price := entry.Price
		hash, err := billing.Hash(price)
		if err != nil {
			return result, err
		}
		sourceHash := catalog.hashes[string(entry.Source)]
		if current.Origin == string(entry.Source) && current.SourceModel == entry.SourceModel && current.SourceHash == sourceHash && activeHashes[name] == hash {
			continue
		}
		publications = append(publications, model.AutomaticBillingPrice{Price: price, Source: string(entry.Source), SourceModel: entry.SourceModel, SourceHash: sourceHash})
	}
	if err := catalog.describeUnconfigured(result); err != nil {
		return result, err
	}
	// A successfully parsed replacement catalog can explicitly reject or remove
	// a formerly priced model. Stop new quotes, but retain immutable old quotes.
	// Download failures kept the previous snapshot and do not reach this case.
	for start := 0; start < len(deactivations); start += 100 {
		end := min(start+100, len(deactivations))
		deactivated, err := model.DeactivateAutomaticBillingPrices(ctx, deactivations[start:end], catalog.hashes)
		if err != nil {
			return result, fmt.Errorf("deactivate unavailable automatic prices: %w", err)
		}
		result.Deactivated += deactivated
	}
	for start := 0; start < len(publications); start += 100 {
		end := min(start+100, len(publications))
		published, err := model.PublishAutomaticBillingPrices(ctx, publications[start:end], catalog.hashes)
		if err != nil {
			return result, fmt.Errorf("publish automatic pricing batch: %w", err)
		}
		result.Published += published
	}
	return result, nil
}

func AutomaticPricingDrafts(ctx context.Context) ([]billing.PriceVersion, []string, string, error) {
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	prices := make([]billing.PriceVersion, 0, len(catalog.entries))
	unconfigured := make([]string, 0, len(catalog.pending))
	for _, entry := range catalog.entries {
		prices = append(prices, entry.Price)
	}
	for name := range catalog.pending {
		unconfigured = append(unconfigured, name)
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i].Model < prices[j].Model })
	sort.Strings(unconfigured)
	versions := []string{}
	for _, spec := range billingPricingSources {
		if hash := catalog.hashes[string(spec.Source)]; hash != "" {
			versions = append(versions, string(spec.Source)+":"+hash)
		}
	}
	return prices, unconfigured, strings.Join(versions, ";"), nil
}

func RestoreAutomaticPrice(ctx context.Context, name string, expectedVersion int64) (*billing.PriceVersion, error) {
	catalog, err := loadAutomaticPriceCatalog(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := catalog.resolve(name)
	if !ok {
		return nil, fmt.Errorf("automatic price is unavailable or ambiguous")
	}
	entry.Price.Model = name
	price, err := ValidateTaskPluginPricePublication(entry.Price, "")
	if err != nil {
		return nil, err
	}
	return model.RestoreAutomaticBillingPrice(ctx, price, string(entry.Source), entry.SourceModel, catalog.hashes[string(entry.Source)], expectedVersion, catalog.hashes)
}
