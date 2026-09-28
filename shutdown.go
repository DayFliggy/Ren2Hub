package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
)

func shutdownApplication(ctx context.Context, server *http.Server) error {
	service.BeginGatewayDrain()
	common.BackgroundWorkers.BeginStop()
	var failures []error
	if err := server.Shutdown(ctx); err != nil {
		failures = append(failures, err)
	}
	// Keep the existing gateway deadline and five-second forced-cancel cleanup.
	if err := service.WaitGatewayDrain(ctx); err != nil {
		failures = append(failures, err, server.Close())
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := common.BackgroundWorkers.Wait(cleanup); err != nil {
		return errors.Join(append(failures, fmt.Errorf("background workers did not stop: %w", err))...)
	}
	if !service.GatewayDrainComplete() {
		return errors.Join(append(failures, errors.New("gateway producers still active; final metrics flush skipped"))...)
	}
	if err := perfmetrics.Shutdown(cleanup); err != nil {
		failures = append(failures, fmt.Errorf("final performance metrics flush failed: %w", err))
	}
	return errors.Join(failures...)
}
