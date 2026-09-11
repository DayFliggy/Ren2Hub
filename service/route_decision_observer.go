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

const recentRouteDecisionLimit = 100

var recentRouteDecisions struct {
	sync.RWMutex
	items []RouteDecision
}

func initRouteDecisionEventQueue() {
	size := common.GetEnvOrDefault("ROUTE_DECISION_EVENT_QUEUE_SIZE", 1024)
	if size < 1 {
		size = 1024
	}
	routeDecisionEventQueue.queue = make(chan RouteDecision, size)
	go func() {
		for decision := range routeDecisionEventQueue.queue {
			recentRouteDecisions.Lock()
			if len(recentRouteDecisions.items) >= recentRouteDecisionLimit {
				copy(recentRouteDecisions.items, recentRouteDecisions.items[1:])
				recentRouteDecisions.items[len(recentRouteDecisions.items)-1] = decision
			} else {
				recentRouteDecisions.items = append(recentRouteDecisions.items, decision)
			}
			recentRouteDecisions.Unlock()
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

func RecentRouteDecisions(limit int) []RouteDecision {
	if limit <= 0 || limit > recentRouteDecisionLimit {
		limit = recentRouteDecisionLimit
	}
	recentRouteDecisions.RLock()
	defer recentRouteDecisions.RUnlock()
	if limit > len(recentRouteDecisions.items) {
		limit = len(recentRouteDecisions.items)
	}
	start := len(recentRouteDecisions.items) - limit
	result := make([]RouteDecision, limit)
	copy(result, recentRouteDecisions.items[start:])
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
