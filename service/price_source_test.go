package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/pricesource"
	"github.com/stretchr/testify/require"
)

func TestFixedPriceDraftsHandlePartialFailureAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"free":{"input_cost_per_token":0,"output_cost_per_token":0},"paid":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`))
		case "/bad":
			w.Write([]byte(`{"invalid":{"input_cost_per_token":-1,"output_cost_per_token":0}}`))
		case "/large":
			w.Write([]byte(strings.Repeat("x", priceDocumentLimit+1)))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	sources := []pricesource.Source{{ID: "sub2api", URL: server.URL + "/ok"}, {ID: "sub2api", URL: server.URL + "/bad"}, {ID: "sub2api", URL: server.URL + "/failure"}, {ID: "sub2api", URL: server.URL + "/large"}}
	rows, err := fetchPriceDrafts(context.Background(), sources, server.Client())
	require.NoError(t, err)
	require.Equal(t, "success", rows[0].Status)
	require.Len(t, rows[0].Models, 2)
	require.Equal(t, "partial", rows[1].Status)
	require.Empty(t, rows[1].Models[0].Pricing)
	require.Equal(t, "error", rows[2].Status)
	require.Nil(t, rows[2].Fingerprint)
	require.NotEmpty(t, rows[2].Errors)
	require.Equal(t, "error", rows[3].Status)
	require.Contains(t, rows[3].Errors[0], "exceeds")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fetchPriceDrafts(ctx, sources, server.Client())
	require.ErrorIs(t, err, context.Canceled)
}

// 发布指纹只做同步锚点：不一致、缺失或读取失败都只警告，不改变草稿状态。
func TestPriceDraftFingerprintWarnsWithoutBlocking(t *testing.T) {
	document := `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}}`
	sum := sha256.Sum256([]byte(document))
	digest := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doc":
			w.Write([]byte(document))
		case "/match.sha256":
			w.Write([]byte(strings.ToUpper(digest) + "  model_prices_and_context_window.json\n"))
		case "/stale.sha256":
			w.Write([]byte(strings.Repeat("0", 64) + "\n"))
		case "/garbage.sha256":
			w.Write([]byte("not a digest"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	source := func(hashPath string) pricesource.Source {
		hashURL := ""
		if hashPath != "" {
			hashURL = server.URL + hashPath
		}
		return pricesource.Source{ID: "sub2api", URL: server.URL + "/doc", HashURL: hashURL}
	}
	rows, err := fetchPriceDrafts(context.Background(), []pricesource.Source{source("/match.sha256"), source("/stale.sha256"), source("/missing.sha256"), source("/garbage.sha256"), source("")}, server.Client())
	require.NoError(t, err)
	for _, row := range rows {
		require.Equal(t, "success", row.Status)
		require.Equal(t, digest, row.Fingerprint.SHA256)
	}
	require.Equal(t, digest, *rows[0].Fingerprint.PublishedSHA256)
	require.True(t, *rows[0].Fingerprint.Matches)
	require.Empty(t, rows[0].Warnings)

	require.False(t, *rows[1].Fingerprint.Matches)
	require.Equal(t, []string{"published sha256 does not match the downloaded document"}, rows[1].Warnings)

	for _, row := range rows[2:4] {
		require.Nil(t, row.Fingerprint.PublishedSHA256)
		require.Nil(t, row.Fingerprint.Matches)
		require.Len(t, row.Warnings, 1)
		require.Contains(t, row.Warnings[0], "published sha256 unavailable")
	}
	require.Nil(t, rows[4].Fingerprint.Matches)
	require.Equal(t, []string{"source does not publish a sha256 fingerprint"}, rows[4].Warnings)
}
