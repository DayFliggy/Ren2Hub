package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/pricesource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type priceDraftTransport func(*http.Request) (*http.Response, error)

func (transport priceDraftTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestModelPriceFetchReportsIndependentSourceResults(t *testing.T) {
	client := &http.Client{Transport: priceDraftTransport(func(request *http.Request) (*http.Response, error) {
		assert.Empty(t, request.Header.Get("Authorization"))
		assert.Empty(t, request.Header.Get("Cookie"))
		status, body := http.StatusOK, ""
		switch request.URL.Host {
		case "valid.test":
			body = `{"success":true,"data":{"model_ratio":{"valid":0}}}`
		case "partial.test":
			body = `{"data":[{"id":"valid","pricing":{"prompt":"0","completion":"0"}},{"id":"invalid","pricing":{"prompt":"-1","completion":"-1"}}]}`
		case "invalid.test":
			body = `{"data":[{"id":"invalid","pricing":{"prompt":"bad","completion":"1"}}]}`
		default:
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	sources := []pricesource.Source{{ID: "basellm", URL: "https://valid.test"}, {ID: "openrouter", URL: "https://partial.test"}, {ID: "openrouter", URL: "https://invalid.test"}, {ID: "sub2api", URL: "https://failed.test"}}
	result := fetchModelPriceDrafts(context.Background(), client, sources, 1024)
	require.Len(t, result.Sources, 4)
	assert.Equal(t, "success", result.Sources[0].Status)
	assert.Equal(t, "partial", result.Sources[1].Status)
	assert.Equal(t, "error", result.Sources[2].Status)
	assert.Equal(t, "error", result.Sources[3].Status)
	assert.Empty(t, result.Sources[0].Errors)
	assert.NotEmpty(t, result.Sources[1].Errors)
	assert.Contains(t, result.Sources[3].Errors[0], "HTTP 503")
	assert.Empty(t, result.Sources[3].Models)
}

func TestModelPriceFetchCancellationAndSizeLimits(t *testing.T) {
	source := pricesource.Source{ID: "basellm", URL: "https://source.test"}
	client := &http.Client{Transport: priceDraftTransport(func(request *http.Request) (*http.Response, error) {
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 65)))}, nil
	})}
	_, err := fetchModelPriceSource(context.Background(), client, source, 64)
	assert.ErrorContains(t, err, "exceeds 64 bytes")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fetchModelPriceSource(ctx, client, source, 64)
	assert.ErrorIs(t, err, context.Canceled)
	_, err = FetchModelPriceDrafts(context.Background(), []string{"unknown"})
	assert.ErrorContains(t, err, "unknown")
}
