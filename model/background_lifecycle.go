package model

import (
	"context"
	"github.com/QuantumNous/new-api/pkg/lifecycle"
)

var modelBackground lifecycle.Workers

// StopBackgroundFlushers stops periodic writers. Final flushes must run after
// all request and task producers have exited.
func StopBackgroundFlushers(ctx context.Context) error { return modelBackground.Stop(ctx) }
