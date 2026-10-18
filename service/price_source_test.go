package service

import (
	"context"
	"github.com/QuantumNous/new-api/pkg/pricesource"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixedPriceDraftsHandlePartialFailureAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"success":true,"data":{"model_price":{"free":0,"paid":0.12}}}`))
		case "/bad":
			w.Write([]byte(`{"success":true,"data":{"model_ratio":{"invalid":-1}}}`))
		case "/large":
			w.Write([]byte(strings.Repeat("x", (10<<20)+1)))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	sources := []pricesource.Source{{ID: "basellm", URL: server.URL + "/ok"}, {ID: "basellm", URL: server.URL + "/bad"}, {ID: "basellm", URL: server.URL + "/failure"}, {ID: "basellm", URL: server.URL + "/large"}}
	rows, err := fetchPriceDrafts(context.Background(), sources, server.Client())
	require.NoError(t, err)
	require.Equal(t, "success", rows[0].Status)
	require.Len(t, rows[0].Models, 2)
	require.Equal(t, "partial", rows[1].Status)
	require.Empty(t, rows[1].Models[0].Pricing)
	require.Equal(t, "error", rows[2].Status)
	require.NotEmpty(t, rows[2].Errors)
	require.Equal(t, "error", rows[3].Status)
	require.Contains(t, rows[3].Errors[0], "10 MiB")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fetchPriceDrafts(ctx, sources, server.Client())
	require.ErrorIs(t, err, context.Canceled)
}
