package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/pkg/pricesource"
)

const modelPriceDocumentLimit = 16 << 20
const modelPriceFetchTimeout = 30 * time.Second

type ModelPriceSourceResult struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Status string              `json:"status"`
	Models []pricesource.Model `json:"models"`
	Errors []string            `json:"errors"`
}

type ModelPriceFetchResult struct {
	Sources []ModelPriceSourceResult `json:"sources"`
}

// FetchModelPriceDrafts returns candidates only. Administrators select a source
// in the editor, and the existing pricing save endpoint remains the sole writer.
func FetchModelPriceDrafts(ctx context.Context, ids []string) (*ModelPriceFetchResult, error) {
	sources, err := pricesource.Select(ids)
	if err != nil {
		return nil, err
	}
	client := newProtectedFetchHTTPClient()
	defer client.CloseIdleConnections()
	// Every destination is fixed. A remote redirect cannot add another destination.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return fetchModelPriceDrafts(ctx, client, sources, modelPriceDocumentLimit), nil
}

func fetchModelPriceDrafts(ctx context.Context, client *http.Client, sources []pricesource.Source, limit int64) *ModelPriceFetchResult {
	result := &ModelPriceFetchResult{Sources: make([]ModelPriceSourceResult, len(sources))}
	var workers sync.WaitGroup
	for i, source := range sources {
		workers.Add(1)
		go func() {
			defer workers.Done()
			row := ModelPriceSourceResult{ID: source.ID, Name: source.Name, Status: "error", Models: []pricesource.Model{}, Errors: []string{}}
			defer func() { result.Sources[i] = row }()
			fetchCtx, cancel := context.WithTimeout(ctx, modelPriceFetchTimeout)
			defer cancel()
			models, err := fetchModelPriceSource(fetchCtx, client, source, limit)
			if err != nil {
				row.Errors = append(row.Errors, err.Error())
				return
			}
			row.Models = models
			supported := 0
			for _, model := range models {
				if model.UnsupportedReason == "" {
					supported++
				}
			}
			if supported == len(models) {
				row.Status = "success"
				return
			}
			row.Errors = append(row.Errors, fmt.Sprintf("%d model prices cannot be imported without changing their meaning", len(models)-supported))
			if supported > 0 {
				row.Status = "partial"
			}
		}()
	}
	workers.Wait()
	return result
}

func fetchModelPriceSource(ctx context.Context, client *http.Client, source pricesource.Source, limit int64) ([]pricesource.Model, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/plain")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("price source request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("price source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("price source response failed: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("price source exceeds %d bytes", limit)
	}
	return pricesource.Parse(source.ID, data)
}
