package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
)

const (
	routeShadowObservationRetention = 14 * 24 * time.Hour
	routeShadowObservationFlush     = time.Second
	routeShadowObservationQueueSize = 2048
)

type routeShadowObservationKey struct {
	HourStart int64
	Scope     string
	ModelName string
}

type routeShadowObservationAggregator struct {
	once       sync.Once
	queue      chan model.RouteShadowHourlyObservation
	instanceID string
	lossMu     sync.Mutex
	losses     map[routeShadowObservationKey]model.RouteShadowHourlyObservation
}

var routeShadowObservationStore routeShadowObservationAggregator

func routeShadowObservationEnabled() bool {
	return model.DB != nil && common.GetEnvOrDefaultBool("ROUTE_SHADOW_ENABLED", false)
}

// StartRouteShadowObservation starts the durable Shadow acceptance observer
// only when Shadow is enabled. It creates one zero-value global row per hour,
// allowing the diagnostics to distinguish an idle hour from missing evidence.
func StartRouteShadowObservation() {
	if !routeShadowObservationEnabled() {
		return
	}
	routeShadowObservationStore.once.Do(routeShadowObservationStore.init)
}

func (s *routeShadowObservationAggregator) init() {
	size := common.GetEnvOrDefault("ROUTE_SHADOW_OBSERVATION_QUEUE_SIZE", routeShadowObservationQueueSize)
	if size < 1 {
		size = routeShadowObservationQueueSize
	}
	s.queue = make(chan model.RouteShadowHourlyObservation, size)
	s.instanceID = uuid.NewString()
	s.losses = make(map[routeShadowObservationKey]model.RouteShadowHourlyObservation)
	go s.run()
}

func (s *routeShadowObservationAggregator) enqueue(delta model.RouteShadowHourlyObservation) {
	if !routeShadowObservationEnabled() {
		return
	}
	s.once.Do(s.init)
	select {
	case s.queue <- delta:
	default:
		// A lost aggregate delta must make the hour unusable for acceptance.
		// The marker is persisted by the flusher or left unsealed on a crash.
		delta.DataLossPossible = true
		s.lossMu.Lock()
		key := routeShadowObservationKey{HourStart: delta.HourStart, Scope: delta.Scope, ModelName: delta.ModelName}
		existing := s.losses[key]
		mergeRouteShadowObservation(&existing, delta)
		existing.DataLossPossible = true
		s.losses[key] = existing
		s.lossMu.Unlock()
	}
}

func (s *routeShadowObservationAggregator) drainLosses() map[routeShadowObservationKey]model.RouteShadowHourlyObservation {
	s.lossMu.Lock()
	defer s.lossMu.Unlock()
	result := s.losses
	s.losses = make(map[routeShadowObservationKey]model.RouteShadowHourlyObservation)
	return result
}

func (s *routeShadowObservationAggregator) run() {
	pending := make(map[routeShadowObservationKey]model.RouteShadowHourlyObservation)
	ticker := time.NewTicker(routeShadowObservationFlush)
	defer ticker.Stop()
	var lastCleanup time.Time
	var lastHeartbeatHour int64
	var lastSealedHour int64

	flush := func() {
		if !routeShadowObservationEnabled() {
			return
		}
		now := time.Now().UTC()
		currentHour := now.Truncate(time.Hour).Unix()
		if lastHeartbeatHour != currentHour {
			addRouteShadowObservationHeartbeat(pending, s.instanceID, currentHour)
		}
		for key, delta := range s.drainLosses() {
			mergeRouteShadowObservationMap(pending, key, delta)
		}
		if len(pending) > 0 {
			rows := make([]model.RouteShadowHourlyObservation, 0, len(pending))
			for _, delta := range pending {
				if delta.InstanceID == "" {
					delta.InstanceID = s.instanceID
				}
				rows = append(rows, delta)
			}
			if err := model.UpsertRouteShadowHourlyObservations(context.Background(), rows); err != nil {
				routeShadowMetrics.AggregateWriteFailures.Add(1)
				return
			}
			pending = make(map[routeShadowObservationKey]model.RouteShadowHourlyObservation)
			lastHeartbeatHour = currentHour
		}

		if lastSealedHour != currentHour {
			if err := model.SealExpiredRouteShadowHourlyObservations(context.Background(), currentHour); err != nil {
				routeShadowMetrics.AggregateWriteFailures.Add(1)
			} else {
				lastSealedHour = currentHour
			}
		}
		if lastCleanup.IsZero() || now.Sub(lastCleanup) >= time.Hour {
			if err := model.DeleteExpiredRouteShadowHourlyObservations(context.Background(), now.Add(-routeShadowObservationRetention).Unix()); err != nil {
				routeShadowMetrics.AggregateWriteFailures.Add(1)
			}
			lastCleanup = now
		}
	}
	flush()

	for {
		select {
		case delta := <-s.queue:
			key := routeShadowObservationKey{HourStart: delta.HourStart, Scope: delta.Scope, ModelName: delta.ModelName}
			mergeRouteShadowObservationMap(pending, key, delta)
		case <-ticker.C:
			flush()
		}
	}
}

func addRouteShadowObservationHeartbeat(pending map[routeShadowObservationKey]model.RouteShadowHourlyObservation, instanceID string, hourStart int64) {
	if pending == nil || strings.TrimSpace(instanceID) == "" || hourStart <= 0 {
		return
	}
	key := routeShadowObservationKey{HourStart: hourStart, Scope: model.RouteShadowObservationGlobal}
	if _, exists := pending[key]; exists {
		return
	}
	pending[key] = model.RouteShadowHourlyObservation{
		HourStart:  hourStart,
		InstanceID: instanceID,
		Scope:      model.RouteShadowObservationGlobal,
	}
}

func mergeRouteShadowObservationMap(target map[routeShadowObservationKey]model.RouteShadowHourlyObservation, key routeShadowObservationKey, delta model.RouteShadowHourlyObservation) {
	existing := target[key]
	mergeRouteShadowObservation(&existing, delta)
	target[key] = existing
}

func mergeRouteShadowObservation(target *model.RouteShadowHourlyObservation, delta model.RouteShadowHourlyObservation) {
	if target == nil {
		return
	}
	if target.HourStart == 0 {
		target.HourStart = delta.HourStart
		target.Scope = delta.Scope
		target.ModelName = delta.ModelName
		target.InstanceID = delta.InstanceID
	}
	target.DataLossPossible = target.DataLossPossible || delta.DataLossPossible
	target.ShadowDecisions += delta.ShadowDecisions
	target.ShadowInitialDecisions += delta.ShadowInitialDecisions
	target.ShadowDiffs += delta.ShadowDiffs
	target.CapabilityResolved += delta.CapabilityResolved
	target.CapabilityUnresolved += delta.CapabilityUnresolved
	target.MappingConflict += delta.MappingConflict
	target.UnknownFiltered += delta.UnknownFiltered
	target.UnknownAdmitted += delta.UnknownAdmitted
	target.MixedDecisions += delta.MixedDecisions
	target.UnauthorizedFiltered += delta.UnauthorizedFiltered
	target.UnauthorizedAdmitted += delta.UnauthorizedAdmitted
	target.SnapshotStale += delta.SnapshotStale
	target.EventAttempted += delta.EventAttempted
	target.EventEnqueued += delta.EventEnqueued
	target.EventDropped += delta.EventDropped
	target.EventEncodeFailed += delta.EventEncodeFailed
	target.EventSubmitted += delta.EventSubmitted
	target.EventWriteFailed += delta.EventWriteFailed
	target.RefreshSuccess += delta.RefreshSuccess
	target.RefreshFailure += delta.RefreshFailure
	target.SnapshotConflict += delta.SnapshotConflict
	target.RefreshLagCount += delta.RefreshLagCount
	target.RefreshLagLE1S += delta.RefreshLagLE1S
	target.RefreshLagLE5S += delta.RefreshLagLE5S
	target.RefreshLagLE15S += delta.RefreshLagLE15S
	target.RefreshLagLE30S += delta.RefreshLagLE30S
	target.RefreshLagLE60S += delta.RefreshLagLE60S
	target.RefreshLagLE120S += delta.RefreshLagLE120S
	target.RefreshLagLE300S += delta.RefreshLagLE300S
	target.RefreshLagGT300S += delta.RefreshLagGT300S
}

func recordRouteShadowObservation(delta model.RouteShadowHourlyObservation, normalizedModel string) {
	if !routeShadowObservationEnabled() {
		return
	}
	hourStart := time.Now().UTC().Truncate(time.Hour).Unix()
	global := delta
	global.HourStart = hourStart
	global.Scope = model.RouteShadowObservationGlobal
	global.ModelName = ""
	routeShadowObservationStore.enqueue(global)
	if normalizedModel == "" {
		return
	}
	perModel := delta
	perModel.HourStart = hourStart
	perModel.Scope = model.RouteShadowObservationModel
	perModel.ModelName = strings.TrimSpace(normalizedModel)
	routeShadowObservationStore.enqueue(perModel)
}

// recordRouteShadowAggregate records the decision before it enters the log
// queue. Event-log backpressure therefore cannot erase decision evidence.
func recordRouteShadowAggregate(decision RouteShadowDecision) {
	recordRouteShadowObservation(routeShadowDecisionObservation(decision), decision.NormalizedRequestModel)
}

func routeShadowDecisionObservation(decision RouteShadowDecision) model.RouteShadowHourlyObservation {
	delta := model.RouteShadowHourlyObservation{ShadowDecisions: 1}
	if decision.RetryAttempt == 0 {
		delta.ShadowInitialDecisions = 1
		if shadowDecisionHasResolvedCapability(decision) {
			delta.CapabilityResolved = 1
		} else if decision.HasMappingConflict {
			delta.MappingConflict = 1
		} else {
			delta.CapabilityUnresolved = 1
		}
	}
	if shadowDecisionHasDifference(decision.DifferenceReasons) {
		delta.ShadowDiffs = 1
	}
	delta.UnknownFiltered = int64(decision.FilterReasonCounts[ShadowFilterUnknownCapability])
	if decision.HasMixed {
		delta.MixedDecisions = 1
	}
	delta.UnauthorizedFiltered = int64(decision.FilterReasonCounts[ShadowFilterGroupForbidden] +
		decision.FilterReasonCounts[ShadowFilterTokenForbidden] +
		decision.FilterReasonCounts[ShadowFilterEntitlementRevoked])
	delta.SnapshotStale = int64(decision.FilterReasonCounts[ShadowFilterSnapshotStale])
	return delta
}

func shadowDecisionHasResolvedCapability(decision RouteShadowDecision) bool {
	for _, candidate := range decision.ShadowCandidates {
		if strings.TrimSpace(candidate.LabSlug) != "" {
			return true
		}
	}
	return false
}

func recordRouteShadowEventObservation(decision RouteShadowDecision, update func(*model.RouteShadowHourlyObservation)) {
	if update == nil {
		return
	}
	delta := model.RouteShadowHourlyObservation{}
	update(&delta)
	recordRouteShadowObservation(delta, decision.NormalizedRequestModel)
}

func recordCapabilityRefreshObservation(success bool, refreshLag time.Duration, cause error) {
	if !routeShadowObservationEnabled() {
		return
	}
	delta := model.RouteShadowHourlyObservation{}
	if success {
		delta.RefreshSuccess = 1
		applyRouteShadowRefreshLag(&delta, refreshLag)
	} else {
		delta.RefreshFailure = 1
		if errors.Is(cause, model.ErrCapabilitySnapshotConflict) {
			delta.SnapshotConflict = 1
		}
	}
	recordRouteShadowObservation(delta, "")
}

func applyRouteShadowRefreshLag(delta *model.RouteShadowHourlyObservation, lag time.Duration) {
	if delta == nil || lag < 0 {
		return
	}
	delta.RefreshLagCount = 1
	switch {
	case lag <= time.Second:
		delta.RefreshLagLE1S = 1
	case lag <= 5*time.Second:
		delta.RefreshLagLE5S = 1
	case lag <= 15*time.Second:
		delta.RefreshLagLE15S = 1
	case lag <= 30*time.Second:
		delta.RefreshLagLE30S = 1
	case lag <= 60*time.Second:
		delta.RefreshLagLE60S = 1
	case lag <= 120*time.Second:
		delta.RefreshLagLE120S = 1
	case lag <= 300*time.Second:
		delta.RefreshLagLE300S = 1
	default:
		delta.RefreshLagGT300S = 1
	}
}
