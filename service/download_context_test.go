package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadRequestContextCancelsOriginAndWorker(t *testing.T) {
	originalClient := httpClient
	httpClient = &http.Client{}
	t.Cleanup(func() { httpClient = originalClient })
	fetch := system_setting.GetFetchSetting()
	originalFetch := *fetch
	originalWorker, originalAllowHTTP := system_setting.WorkerUrl, system_setting.WorkerAllowHttpImageRequestEnabled
	fetch.EnableSSRFProtection = false
	system_setting.WorkerAllowHttpImageRequestEnabled = true
	t.Cleanup(func() {
		*fetch = originalFetch
		system_setting.WorkerUrl, system_setting.WorkerAllowHttpImageRequestEnabled = originalWorker, originalAllowHTTP
	})
	for _, worker := range []bool{false, true} {
		t.Run(map[bool]string{false: "origin", true: "worker"}[worker], func(t *testing.T) {
			started := make(chan struct{})
			canceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if worker {
					assert.Equal(t, http.MethodPost, r.Method)
					_, _ = io.Copy(io.Discard, r.Body)
				}
				close(started)
				<-r.Context().Done()
				close(canceled)
			}))
			defer server.Close()
			system_setting.WorkerUrl = ""
			if worker {
				system_setting.WorkerUrl = server.URL
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				resp, err := DoDownloadRequestWithContext(ctx, server.URL+"/image.png")
				if resp != nil {
					_ = resp.Body.Close()
				}
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("download did not reach the local server")
			}
			cancel()
			select {
			case err := <-finished:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(3 * time.Second):
				t.Fatal("download did not stop after cancellation")
			}
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("download cancellation did not reach upstream")
			}
		})
	}
}

func TestImageDownloadContextCancelsBodyRead(t *testing.T) {
	originalLimit := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = originalLimit })
	originalClient := httpClient
	httpClient = &http.Client{}
	t.Cleanup(func() { httpClient = originalClient })
	fetch := system_setting.GetFetchSetting()
	originalFetch := *fetch
	originalWorker := system_setting.WorkerUrl
	fetch.EnableSSRFProtection = false
	system_setting.WorkerUrl = ""
	t.Cleanup(func() { *fetch, system_setting.WorkerUrl = originalFetch, originalWorker })
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, body, err := GetImageFromUrlWithContext(ctx, server.URL)
		assert.Empty(t, body)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("image response did not start")
	}
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("image body read did not stop after cancellation")
	}
}

func TestDownloadRequestContextPreservesSSRFRejection(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	originalWorker := system_setting.WorkerUrl
	system_setting.WorkerUrl = ""
	t.Cleanup(func() { system_setting.WorkerUrl = originalWorker })
	resp, err := DoDownloadRequestWithContext(context.Background(), "http://127.0.0.1/image.png")
	require.Error(t, err)
	assert.Nil(t, resp)
}
