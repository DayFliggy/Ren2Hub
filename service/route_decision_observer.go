package service

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

var routeDecisionEventQueue struct {
	sync.Once
	queue chan RouteDecision
}

var routeDecisionEventsDropped atomic.Uint64

func initRouteDecisionEventQueue() {
	size := common.GetEnvOrDefault("ROUTE_DECISION_EVENT_QUEUE_SIZE", 1024)
	if size < 1 {
		size = 1024
	}
	routeDecisionEventQueue.queue = make(chan RouteDecision, size)
	go func() {
		for decision := range routeDecisionEventQueue.queue {
			data, err := common.Marshal(decision)
			if err != nil {
				continue
			}
			requestContext := context.WithValue(context.Background(), common.RequestIdKey, decision.RequestID)
			logger.LogInfo(requestContext, string(data))
		}
	}()
}

func EnqueueRouteDecision(decision RouteDecision) {
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
