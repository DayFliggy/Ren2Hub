package service

import (
	"context"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
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
				recordRouteShadowEventObservation(decision, func(delta *model.RouteShadowHourlyObservation) {
					delta.EventEncodeFailed = 1
				})
				continue
			}
			requestContext := context.WithValue(context.Background(), common.RequestIdKey, decision.RequestID)
			if err := logger.TryLogInfo(requestContext, string(data)); err != nil {
				observeShadowEventWriteFailure()
				recordRouteShadowEventObservation(decision, func(delta *model.RouteShadowHourlyObservation) {
					delta.EventWriteFailed = 1
				})
				continue
			}
			observeShadowEventWritten()
			recordRouteShadowEventObservation(decision, func(delta *model.RouteShadowHourlyObservation) {
				delta.EventSubmitted = 1
			})
		}
	}()
}

func EnqueueRouteShadowDecision(_ context.Context, decision RouteShadowDecision) {
	routeShadowEventQueue.Do(initRouteShadowEventQueue)
	observeShadowEventAttempt()
	recordRouteShadowEventObservation(decision, func(delta *model.RouteShadowHourlyObservation) {
		delta.EventAttempted = 1
	})
	select {
	case routeShadowEventQueue.queue <- decision:
		recordRouteShadowEventObservation(decision, func(delta *model.RouteShadowHourlyObservation) {
			delta.EventEnqueued = 1
		})
	default:
		routeShadowMetrics.EventsDropped.Add(1)
		recordRouteShadowEventObservation(decision, func(delta *model.RouteShadowHourlyObservation) {
			delta.EventDropped = 1
		})
	}
}
