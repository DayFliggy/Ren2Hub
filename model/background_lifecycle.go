package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/lifecycle"
)

var modelBackground lifecycle.Workers
var modelCacheUpdates lifecycle.Deferred

// StopBackgroundFlushers stops periodic writers. Final flushes must run after
// all request and task producers have exited.
func StopBackgroundFlushers(ctx context.Context) error { return modelBackground.Stop(ctx) }

// Cache mutations accepted by an in-flight request must finish before the
// final accounting flush. Seal this set only after request producers stop.
func StopCacheUpdates(ctx context.Context) error { return modelCacheUpdates.Stop(ctx) }

func queueModelCacheUpdate(update func()) {
	if !modelCacheUpdates.Go(update) {
		common.SysError("cache mutation rejected after producers stopped")
	}
}
