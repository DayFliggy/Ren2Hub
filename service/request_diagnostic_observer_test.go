package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRequestDiagnosticObserverFailureDoesNotBlockRequests(t *testing.T) {
	// The process-wide observer starts once. A subprocess exercises its real
	// queue and shutdown without resetting globals used by unrelated tests.
	const marker = "REN2HUB_DIAGNOSTIC_OBSERVER_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRequestDiagnosticObserverFailureDoesNotBlockRequests$", "-test.timeout=10s")
		child.Env = append(os.Environ(), marker+"=1")
		output, err := child.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	t.Setenv("ROUTE_DECISION_EVENT_QUEUE_SIZE", "1")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "observer.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	model.DB = db
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	t.Cleanup(func() {
		common.BackgroundWorkers.BeginStop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, common.BackgroundWorkers.Wait(ctx))
	})

	started, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("block_diagnostic_storage", func(tx *gorm.DB) {
		first.Do(func() {
			close(started)
			select {
			case <-release:
			case <-tx.Statement.Context.Done():
			}
		})
	}))
	decision := NewRouteDecision("observer-first", RouteSourceAutoLab, "model", 1)
	decision.UserID = 1
	EnqueueRouteDecision(decision)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("observer did not reach storage")
	}
	decision.RequestID = "observer-queued"
	EnqueueRouteDecision(decision)
	returned := make(chan struct{})
	go func() {
		decision.RequestID = "observer-dropped"
		EnqueueRouteDecision(decision)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("a full diagnostic queue blocked the request")
	}
	assert.Equal(t, uint64(1), RouteDecisionEventsDropped())
	close(release)
	// The diagnostic table deliberately does not exist. Both accepted events
	// must expose storage failures instead of pretending they were persisted.
	require.Eventually(t, func() bool { return RouteDecisionStorageFailures() == 2 }, 3*time.Second, 10*time.Millisecond)
	common.BackgroundWorkers.BeginStop()
	EnqueueRouteDecision(decision)
	assert.Equal(t, uint64(2), RouteDecisionEventsDropped(), "shutdown also rejects new observation work")
}
