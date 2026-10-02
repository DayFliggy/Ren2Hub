package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

var routeDecisionEventQueue struct {
	sync.Once
	queue chan RouteDecision
}

var routeDecisionEventsDropped atomic.Uint64
var routeDecisionStorageFailed atomic.Uint64

func initRouteDecisionEventQueue() {
	size := common.GetEnvOrDefault("ROUTE_DECISION_EVENT_QUEUE_SIZE", 1024)
	if size < 1 {
		size = 1024
	}
	routeDecisionEventQueue.queue = make(chan RouteDecision, size)
	common.BackgroundWorkers.Go(func(ctx context.Context) {
		for {
			var decision RouteDecision
			select {
			case <-ctx.Done():
				return
			case decision = <-routeDecisionEventQueue.queue:
			}
			data, err := common.Marshal(decision)
			if err != nil {
				routeDecisionStorageFailed.Add(1)
				common.SysError("route decision serialization failed")
				continue
			}
			requestContext := context.WithValue(context.Background(), common.RequestIdKey, decision.RequestID)
			logger.LogInfo(requestContext, string(data))
			writeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err := PersistRouteDecision(writeContext, decision); err != nil {
				routeDecisionStorageFailed.Add(1)
				common.SysError("route decision persistence failed")
			}
			cancel()
		}
	})
}

func EnqueueRouteDecision(decision RouteDecision) {
	if !common.BackgroundWorkers.Accepting() {
		routeDecisionEventsDropped.Add(1)
		return
	}
	routeDecisionEventQueue.Do(initRouteDecisionEventQueue)
	select {
	case routeDecisionEventQueue.queue <- decision:
	default:
		// Decision logging is observability only and must never delay or fail a
		// request when the bounded queue is full.
		routeDecisionEventsDropped.Add(1)
	}
}

func RouteDecisionEventsDropped() uint64 {
	return routeDecisionEventsDropped.Load()
}

func RouteDecisionStorageFailures() uint64 { return routeDecisionStorageFailed.Load() }
