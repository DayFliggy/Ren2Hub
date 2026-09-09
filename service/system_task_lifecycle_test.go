package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemTaskShutdownCancelsHandlerAndWaitsForTerminalWrite(t *testing.T) {
	truncate(t)
	saved := systemTaskWorkers
	systemTaskWorkers = &lifecycle.Workers{}
	t.Cleanup(func() { systemTaskWorkers = saved })
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	handler := &stubScheduledHandler{taskType: "shutdown-fixture", onRun: func(ctx context.Context, task *model.SystemTask, runnerID string) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		finished <- model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "server shutdown")
	}}
	withSystemTaskRegistry(t, handler)
	task, err := model.CreateSystemTask(handler.Type(), nil, nil)
	require.NoError(t, err)
	runSystemTaskClaimPass("shutdown-runner")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- StopSystemTaskRunner(ctx) }()
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal("handler was not canceled")
	}
	select {
	case <-stopped:
		t.Fatal("shutdown did not wait for handler persistence")
	default:
	}
	close(release)
	require.NoError(t, <-finished)
	require.NoError(t, <-stopped)
	_, _, err = EnqueueSystemTask(handler.Type(), nil)
	require.Error(t, err)
	row, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, row.Status)
}
