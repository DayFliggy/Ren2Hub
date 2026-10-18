package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/pkg/pricesource"
)

type PriceSourceDraft struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Status string              `json:"status"`
	Models []pricesource.Model `json:"models"`
	Errors []string            `json:"errors"`
}

// Fixed sources contain no credentials and never accept caller-provided URLs.
// Fetching only creates editable drafts; publishing uses native pricing APIs.
func FetchPriceDrafts(ctx context.Context, ids []string) ([]PriceSourceDraft, error) {
	sources, err := pricesource.Select(ids)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return fetchPriceDrafts(ctx, sources, client)
}

func fetchPriceDrafts(ctx context.Context, sources []pricesource.Source, client *http.Client) ([]PriceSourceDraft, error) {
	results := make([]PriceSourceDraft, 0, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result := PriceSourceDraft{ID: source.ID, Name: source.Name, Status: "success", Models: []pricesource.Model{}, Errors: []string{}}
		models, err := fetchPriceSource(ctx, source, client)
		if err != nil {
			result.Status = "error"
			result.Errors = append(result.Errors, err.Error())
		} else {
			result.Models = models
			for _, row := range models {
				if row.UnsupportedReason != "" {
					result.Status = "partial"
					break
				}
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func fetchPriceSource(ctx context.Context, source pricesource.Source, client *http.Client) ([]pricesource.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("price source returned HTTP %d", response.StatusCode)
	}
	const limit = 10 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, fmt.Errorf("price source exceeds 10 MiB")
	}
	return pricesource.Parse(source.ID, body)
}
