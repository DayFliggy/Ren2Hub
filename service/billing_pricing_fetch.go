package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/autopricing"
)

const maxPricingDocumentBytes = 16 << 20

func pricingDocumentHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

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
	if validCache {
		_, parseErr := autopricing.Parse(spec.Source, []byte(previous.Document))
		validCache = parseErr == nil
	}
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
	updated.URL, updated.RemoteHash, updated.ParserVersion = spec.URL, remoteHash, autopricing.ParserVersion
	updated.ETag, updated.LastModified = headers.Get("ETag"), headers.Get("Last-Modified")
	updated.ModelCount = len(entries)
	changed := !validCache || hash != previous.Hash
	if changed {
		updated.UpdatedAt = time.Now().Unix()
	}
	return updated, changed, nil
}
