//go:build known_upstream_restored

package controller

import (
	"context"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRestoredNativeControlModelFetchCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-model"}]}`))
	}))
	defer server.Close()
	service.InitHttpClient()
	url := server.URL
	ch := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "fixture-key", BaseURL: &url}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = restoredControlFetchModels(ctx, ch)
	if calls.Load() != 0 {
		t.Error("CONTROL_MANAGEMENT_CANCEL: cancelled management request still dispatched")
	}
}
