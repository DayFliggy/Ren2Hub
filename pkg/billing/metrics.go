package billing

import (
	"sync/atomic"
	"time"
)

var operationalCounters = struct {
	startedAt           int64
	sourceFailures      [3]atomic.Uint64
	candidateRejections [3]atomic.Uint64
	missingPrice        atomic.Uint64
	unknownUsage        atomic.Uint64
	calculations        atomic.Uint64
	calculationFailures atomic.Uint64
	replayMismatches    atomic.Uint64
}{startedAt: time.Now().Unix()}

type OperationalMetrics struct {
	CounterScope          string            `json:"counter_scope"`
	StartedAt             int64             `json:"started_at"`
	SourceRefreshFailures map[string]uint64 `json:"source_refresh_failures_total"`
	CandidateRejections   map[string]uint64 `json:"candidate_rejections_total"`
	MissingPrice          uint64            `json:"missing_price_total"`
	UnknownUsage          uint64            `json:"unknown_usage_total"`
	Calculations          uint64            `json:"calculation_total"`
	CalculationFailures   uint64            `json:"calculation_failures_total"`
	ReplayMismatches      uint64            `json:"replay_mismatches_total"`
}

func sourceMetricIndex(source string) int {
	switch source {
	case "sub2api":
		return 0
	case "axonhub":
		return 1
	default:
		return 2
	}
}

func RecordSourceRefreshFailure(source string) {
	operationalCounters.sourceFailures[sourceMetricIndex(source)].Add(1)
}
func RecordCandidateRejection(source string) {
	operationalCounters.candidateRejections[sourceMetricIndex(source)].Add(1)
}
func RecordMissingPrice() { operationalCounters.missingPrice.Add(1) }
func RecordUnknownUsage() { operationalCounters.unknownUsage.Add(1) }
func RecordCalculation(success bool) {
	operationalCounters.calculations.Add(1)
	if !success {
		operationalCounters.calculationFailures.Add(1)
	}
}
func RecordReplayMismatch() { operationalCounters.replayMismatches.Add(1) }

// Counters belong to this process. Source is restricted to three fixed values;
// request IDs, models, hashes, channels and error messages never become labels.
func ReadOperationalMetrics() OperationalMetrics {
	result := OperationalMetrics{CounterScope: "instance", StartedAt: operationalCounters.startedAt,
		SourceRefreshFailures: make(map[string]uint64, 3), CandidateRejections: make(map[string]uint64, 3),
		MissingPrice: operationalCounters.missingPrice.Load(), UnknownUsage: operationalCounters.unknownUsage.Load(),
		Calculations: operationalCounters.calculations.Load(), CalculationFailures: operationalCounters.calculationFailures.Load(), ReplayMismatches: operationalCounters.replayMismatches.Load()}
	for i, source := range []string{"sub2api", "axonhub", "other"} {
		result.SourceRefreshFailures[source] = operationalCounters.sourceFailures[i].Load()
		result.CandidateRejections[source] = operationalCounters.candidateRejections[i].Load()
	}
	return result
}
