package service

import (
	"context"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

var routeShadowEventQueue struct {
	sync.Once
	queue chan RouteShadowDecision
}

func initRouteShadowEventQueue() {
	size := common.GetEnvOrDefault("ROUTE_SHADOW_EVENT_QUEUE_SIZE", 1024)
	if size < 1 {
		size = 1024
	}
	routeShadowEventQueue.queue = make(chan RouteShadowDecision, size)
	go func() {
		for decision := range routeShadowEventQueue.queue {
			data, err := common.Marshal(decision)
			if err != nil {
				observeShadowEventEncodeFailure()
				continue
			}
			// The durable, cross-instance model counters are updated by the
			// bounded observer worker, never on the request hot path.
			recordRouteShadowAggregate(decision)
			requestContext := context.WithValue(context.Background(), common.RequestIdKey, decision.RequestID)
			if err := logger.TryLogInfo(requestContext, string(data)); err != nil {
				observeShadowEventWriteFailure()
				continue
			}
			observeShadowEventWritten()
		}
	}()
}

func EnqueueRouteShadowDecision(_ context.Context, decision RouteShadowDecision) {
	routeShadowEventQueue.Do(initRouteShadowEventQueue)
	observeShadowEventAttempt()
	select {
	case routeShadowEventQueue.queue <- decision:
	default:
		routeShadowMetrics.EventsDropped.Add(1)
	}
}
