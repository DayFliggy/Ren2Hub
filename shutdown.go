package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/lifecycle"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
)

var applicationRequests lifecycle.Requests

func drainApplication(ctx context.Context, server *http.Server) error {
	applicationRequests.Seal()
	if err := lifecycle.StopAll(ctx,
		lifecycle.Stopper{Name: "requests", Stop: applicationRequests.Stop},
		lifecycle.Stopper{Name: "background workers", Stop: common.BackgroundWorkers.Stop},
		lifecycle.Stopper{Name: "system tasks", Stop: service.StopSystemTaskRunner},
		lifecycle.Stopper{Name: "accounting flushers", Stop: model.StopBackgroundFlushers},
		lifecycle.Stopper{Name: "metrics flusher", Stop: perfmetrics.Stop},
		lifecycle.Stopper{Name: "HTTP server", Stop: func(ctx context.Context) error {
			if err := server.Shutdown(ctx); err != nil {
				return errors.Join(err, server.Close())
			}
			return nil
		}},
	); err != nil {
		return fmt.Errorf("request or worker drain incomplete: %w", err)
	}
	if err := common.DeferredWorkers.Stop(ctx); err != nil {
		return fmt.Errorf("deferred mutations did not drain: %w", err)
	}
	if err := model.StopCacheUpdates(ctx); err != nil {
		return fmt.Errorf("cache mutations did not drain: %w", err)
	}
	// No producers may remain when frozen batches and the current bucket flush.
	return errors.Join(model.FlushBatchUpdates(ctx), model.SaveQuotaDataCacheContext(ctx), perfmetrics.Flush(ctx))
}
