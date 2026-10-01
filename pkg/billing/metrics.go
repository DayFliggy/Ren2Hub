package billing

import (
	"sync/atomic"
	"time"
)

// Settlement bases describe how exact a final charge is. Root reads them in
// gateway metrics instead of reviewing individual requests.
const (
	SettlementConfirmed      = "confirmed"
	SettlementEstimated      = "estimated"
	SettlementFrozenEstimate = "frozen_estimate"
)

var settlementBases = [...]string{SettlementConfirmed, SettlementEstimated, SettlementFrozenEstimate}

var operationalCounters = struct {
	startedAt           int64
	sourceFailures      [3]atomic.Uint64
	candidateRejections [3]atomic.Uint64
	missingPrice        atomic.Uint64
	calculations        atomic.Uint64
	calculationFailures atomic.Uint64
	settlements         [len(settlementBases)]atomic.Uint64
}{startedAt: time.Now().Unix()}

type OperationalMetrics struct {
	CounterScope          string            `json:"counter_scope"`
	StartedAt             int64             `json:"started_at"`
	SourceRefreshFailures map[string]uint64 `json:"source_refresh_failures_total"`
	CandidateRejections   map[string]uint64 `json:"candidate_rejections_total"`
	MissingPrice          uint64            `json:"missing_price_total"`
	Calculations          uint64            `json:"calculation_total"`
	CalculationFailures   uint64            `json:"calculation_failures_total"`
	Settlements           map[string]uint64 `json:"settlements_total"`
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
func RecordCalculation(success bool) {
	operationalCounters.calculations.Add(1)
	if !success {
		operationalCounters.calculationFailures.Add(1)
	}
}

// RecordSettlement counts a committed final charge by its evidence basis.
func RecordSettlement(basis string) {
	for i, known := range settlementBases {
		if known == basis {
			operationalCounters.settlements[i].Add(1)
			return
		}
	}
}

// SettlementBasis classifies the usage that became a final charge.
func SettlementBasis(usage *Usage) string {
	switch {
	case usage == nil:
		return SettlementEstimated
	case usage.Confirmed():
		return SettlementConfirmed
	case usage.Source == UsageSourceRequestEstimate:
		return SettlementFrozenEstimate
	default:
		return SettlementEstimated
	}
}

// Counters belong to this process. Source and basis are fixed values; request
// IDs, models, hashes, channels and error messages never become labels.
func ReadOperationalMetrics() OperationalMetrics {
	result := OperationalMetrics{CounterScope: "instance", StartedAt: operationalCounters.startedAt,
		SourceRefreshFailures: make(map[string]uint64, 3), CandidateRejections: make(map[string]uint64, 3),
		MissingPrice: operationalCounters.missingPrice.Load(), Calculations: operationalCounters.calculations.Load(),
		CalculationFailures: operationalCounters.calculationFailures.Load(), Settlements: make(map[string]uint64, len(settlementBases))}
	for i, source := range []string{"sub2api", "axonhub", "other"} {
		result.SourceRefreshFailures[source] = operationalCounters.sourceFailures[i].Load()
		result.CandidateRejections[source] = operationalCounters.candidateRejections[i].Load()
	}
	for i, basis := range settlementBases {
		result.Settlements[basis] = operationalCounters.settlements[i].Load()
	}
	return result
}
