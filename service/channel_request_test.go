package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestChannelRequestBusyNeverSendsAndCloseReleasesOnce(t *testing.T) {
	setupRouteTest(t)
	ch := &model.Channel{Id: 11, MaxConcurrency: 1}
	sends := 0
	send := func() (*http.Response, error) {
		sends++
		return &http.Response{Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}
	first, err := DoChannelRequest(context.Background(), ch, send)
	require.NoError(t, err)
	_, err = DoChannelRequest(context.Background(), ch, send)
	require.ErrorIs(t, err, ErrConcurrencyFull)
	require.Equal(t, 1, sends)
	require.NoError(t, first.Body.Close())
	require.NoError(t, first.Body.Close())
	require.Zero(t, ChannelInFlight(11))
	_, err = DoChannelRequest(context.Background(), ch, func() (*http.Response, error) { return nil, errors.New("upstream failed") })
	require.Error(t, err)
	require.Zero(t, ChannelInFlight(11))
}

func TestChannelRequestCancellationReleasesAndDoesNotSendWhenAlreadyCancelled(t *testing.T) {
	setupRouteTest(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := &model.Channel{Id: 11, MaxConcurrency: 1}
	done := make(chan error, 1)
	go func() {
		_, err := DoChannelRequest(ctx, ch, func() (*http.Response, error) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			return http.DefaultClient.Do(req)
		})
		done <- err
	}()
	<-started
	require.Equal(t, 1, ChannelInFlight(11))
	cancel()
	require.Error(t, <-done)
	require.Zero(t, ChannelInFlight(11))
	_, err := DoChannelRequest(ctx, ch, func() (*http.Response, error) { t.Fatal("cancelled request sent"); return nil, nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestTaskPollingBusyIsNotAnUpstreamFailure(t *testing.T) {
	setupRouteTest(t)
	ch := &model.Channel{Id: 11, MaxConcurrency: 1}
	p, err := AcquireChannelConcurrency(context.Background(), ch)
	require.NoError(t, err)
	defer p.Release()
	task := &model.Task{TaskID: "task", ChannelId: 11, Status: model.TaskStatusInProgress}
	adaptor := &taskPollingFetchAdaptor{}
	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, "task", map[string]*model.Task{"task": task}))
	require.Empty(t, adaptor.taskIDs)
	require.Zero(t, task.PrivateData.PollFailures)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
}
