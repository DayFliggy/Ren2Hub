package service

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
)

type gatewayQueueCounters struct {
	active    atomic.Int64
	entered   atomic.Uint64
	completed atomic.Uint64
	failed    atomic.Uint64
	waitMS    atomic.Int64
}

type GatewayQueueMetrics struct {
	Active    int64  `json:"active"`
	Entered   uint64 `json:"entered_total"`
	Completed uint64 `json:"completed_total"`
	Failed    uint64 `json:"failed_total"`
	WaitMS    int64  `json:"wait_ms_total"`
}

func (counter *gatewayQueueCounters) snapshot() GatewayQueueMetrics {
	return GatewayQueueMetrics{
		Active: counter.active.Load(), Entered: counter.entered.Load(), Completed: counter.completed.Load(),
		Failed: counter.failed.Load(), WaitMS: counter.waitMS.Load(),
	}
}

var gatewayMetrics = struct {
	startedAt            int64
	user                 gatewayQueueCounters
	channel              gatewayQueueCounters
	stateBindingFailures atomic.Uint64
}{startedAt: time.Now().Unix()}

func gatewayQueueCounter(resourceKey string) *gatewayQueueCounters {
	switch {
	case strings.HasPrefix(resourceKey, "user:"):
		return &gatewayMetrics.user
	case strings.HasPrefix(resourceKey, "channel:"):
		return &gatewayMetrics.channel
	default:
		return nil
	}
}

type GatewayMetricsSnapshot struct {
	RouteDecisionEventsDropped uint64                       `json:"route_decision_events_dropped_total"`
	RouteDecisionStorageFailed uint64                       `json:"route_decision_storage_failed_total"`
	SchemaVersion              int                          `json:"schema_version"`
	Admission                  AdmissionMetrics             `json:"admission"`
	RetryBudget                RetryBudgetSnapshot          `json:"retry_budget"`
	Maintenance                AttemptMaintenanceMetrics    `json:"attempt_maintenance"`
	CounterScope               string                       `json:"counter_scope"`
	StartedAt                  int64                        `json:"started_at"`
	UserQueue                  GatewayQueueMetrics          `json:"user_queue"`
	ChannelQueue               GatewayQueueMetrics          `json:"channel_queue"`
	StateBindingFailures       uint64                       `json:"state_binding_failure_total"`
	RecoveryScope              string                       `json:"recovery_scope"`
	Recovery                   model.BillingRecoveryBacklog `json:"billing_recovery"`
	// Billing counts settlements by evidence basis, the operator view that
	// replaces per-request reconciliation.
	Billing billing.OperationalMetrics `json:"billing"`
}

// Counters belong to this process; the recovery gauge belongs to the shared
// database. No request, user, channel, credential, or model becomes a label.
func ReadGatewayMetrics(ctx context.Context) (GatewayMetricsSnapshot, error) {
	backlog, err := model.ReadBillingRecoveryBacklog(ctx, time.Now().Add(-BillingRecoveryStaleAfter()).Unix())
	if err != nil {
		return GatewayMetricsSnapshot{}, err
	}
	if backlog.SettlementBlocked, err = model.CountSettlementBlocked(ctx); err != nil {
		return GatewayMetricsSnapshot{}, err
	}
	return GatewayMetricsSnapshot{
		RouteDecisionEventsDropped: RouteDecisionEventsDropped(),
		RouteDecisionStorageFailed: RouteDecisionStorageFailures(),
		CounterScope:               "instance", StartedAt: gatewayMetrics.startedAt,
		Maintenance: ReadAttemptMaintenanceMetrics(),
		UserQueue:   gatewayMetrics.user.snapshot(), ChannelQueue: gatewayMetrics.channel.snapshot(),
		SchemaVersion: 2, Admission: gatewayAdmission.Metrics(), RetryBudget: gatewayRetries.Snapshot(), StateBindingFailures: gatewayMetrics.stateBindingFailures.Load(),
		RecoveryScope: "database", Recovery: backlog, Billing: billing.ReadOperationalMetrics(),
	}, nil
}
