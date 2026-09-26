package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autopricing"
)

//go:embed pricingdata/*.json.gz
var bundledPricingSources embed.FS

type billingPricingSourceSpec struct {
	Source  autopricing.SourceID
	URL     string
	HashURL string
	Role    string
}

// Source priority applies to a complete model price, never individual fields.
var billingPricingSources = []billingPricingSourceSpec{
	{autopricing.SourceSub2API, "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json", "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.sha256", "primary"},
	{autopricing.SourceAxonHub, "https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json", "", "fallback"},
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
	SnapshotState string `json:"snapshot_state"`
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

// InitializeAutomaticPricing performs no network I/O and never replaces a
// persisted snapshot. Ownership migration also runs when publication is off.
func InitializeAutomaticPricing(ctx context.Context) error {
	if err := model.MigrateBillingPriceOrigins(ctx); err != nil {
		return err
	}
	rows, err := model.ListBillingPricingSources(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]bool, len(rows))
	for _, row := range rows {
		existing[row.Source] = true
	}
	for _, spec := range billingPricingSources {
		if existing[string(spec.Source)] {
			continue
		}
		compressed, err := bundledPricingSources.ReadFile("pricingdata/" + string(spec.Source) + ".json.gz")
		if err != nil {
			return err
		}
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxPricingDocumentBytes+1))
		closeErr := reader.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if len(data) > maxPricingDocumentBytes {
			return fmt.Errorf("bundled pricing source exceeds limit")
		}
		entries, err := autopricing.Parse(spec.Source, data)
		if err != nil {
			return fmt.Errorf("bundled %s: %w", spec.Source, err)
		}
		row := model.BillingPricingSource{Source: string(spec.Source), Document: model.LongText(data), Hash: pricingDocumentHash(data), ModelCount: len(entries)}
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
	bySource := make(map[string]model.BillingPricingSource, len(rows))
	for _, row := range rows {
		bySource[row.Source] = row
	}
	result := &AutomaticPricingStatus{Config: config, Origins: origins, Sources: []AutomaticPricingSourceStatus{}}
	for _, spec := range billingPricingSources {
		row := bySource[string(spec.Source)]
		status := AutomaticPricingSourceStatus{Source: string(spec.Source), URL: spec.URL, Role: spec.Role, Hash: row.Hash, CheckedAt: row.CheckedAt, UpdatedAt: row.UpdatedAt, LastError: row.LastError, SnapshotState: "unavailable"}
		if row.Document != "" {
			entries, rejected, parseErr := autopricing.ParseWithReport(spec.Source, []byte(row.Document))
			if pricingDocumentHash([]byte(row.Document)) != row.Hash {
				status.LastError = "cached pricing document integrity mismatch"
			} else if parseErr != nil {
				status.LastError = "cached pricing document is invalid"
			} else {
				status.ModelCount, status.RejectedCount = len(entries), len(rejected)
				status.SnapshotState = "current"
				if row.LastError != "" {
					status.SnapshotState = "cached"
				} else if row.CheckedAt == 0 {
					status.SnapshotState = "bundled"
				}
			}
		}
		result.Sources = append(result.Sources, status)
	}
	return result, nil
}

func SyncAutomaticPricing(ctx context.Context) (*AutomaticPricingSyncResult, error) {
	return syncAutomaticPricing(ctx, newProtectedFetchHTTPClient(), billingPricingSources)
}

func syncAutomaticPricing(ctx context.Context, client *http.Client, sources []billingPricingSourceSpec) (*AutomaticPricingSyncResult, error) {
	result := &AutomaticPricingSyncResult{Unconfigured: []string{}, UnconfiguredDetails: []AutomaticPricingUnconfigured{}, Errors: []string{}}
	if err := model.MigrateBillingPriceOrigins(ctx); err != nil {
		return result, err
	}
	for _, spec := range billingPricingSources {
		if err := model.EnsureBillingPricingSource(ctx, &model.BillingPricingSource{Source: string(spec.Source)}); err != nil {
			return result, err
		}
	}
	rows, err := model.ListBillingPricingSources(ctx)
	if err != nil {
		return result, err
	}
	previous := make(map[string]model.BillingPricingSource, len(rows))
	for _, row := range rows {
		previous[row.Source] = row
	}
	var failures []error
	for _, spec := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if spec.Source != autopricing.SourceSub2API && spec.Source != autopricing.SourceAxonHub {
			return result, fmt.Errorf("unsupported pricing source %q", spec.Source)
		}
		old := previous[string(spec.Source)]
		old.Source = string(spec.Source)
		row, changed, fetchErr := fetchPricingSource(ctx, client, spec, old)
		row.CheckedAt, row.LastError = time.Now().Unix(), ""
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
	// A previous run may have saved its snapshot but failed before publication.
	publication, err := publishAutomaticPricingCatalog(ctx)
	if publication != nil {
		result.Published, result.Deactivated = publication.Published, publication.Deactivated
		result.Unconfigured, result.UnconfiguredCount = publication.Unconfigured, publication.UnconfiguredCount
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
