package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type RouteSource string

const (
	RouteSourceUnavailable RouteSource = "unavailable"
	RouteSourceManual      RouteSource = "manual"
	RouteSourceAutoLab     RouteSource = "auto_lab"
)

type RouteSourceInput struct {
	CapabilityEnabled bool
	HasProfile        bool
	ProfileMode       string
}

// ResolveRouteSource is the only route-mode precedence rule. It is pure so
// middleware, preview, and the request selector cannot disagree about
// which source owns a request.
func ResolveRouteSource(input RouteSourceInput) RouteSource {
	if !input.CapabilityEnabled {
		return RouteSourceUnavailable
	}
	if !input.HasProfile {
		return RouteSourceAutoLab
	}
	switch input.ProfileMode {
	case "manual":
		return RouteSourceManual
	case "auto_lab":
		return RouteSourceAutoLab
	default:
		return RouteSourceUnavailable
	}
}

type RouteDecisionCandidate struct {
	ChannelID       int                  `json:"channel_id"`
	RequestModel    string               `json:"request_model,omitempty"`
	FilterReason    string               `json:"filter_reason,omitempty"`
	Priority        int64                `json:"priority"`
	Position        int                  `json:"position"`
	Weight          int                  `json:"weight"`
	SnapshotVersion int64                `json:"snapshot_version,omitempty"`
	HealthEpoch     int64                `json:"health_epoch,omitempty"`
	CatalogVersion  string               `json:"catalog_version,omitempty"`
	Score           *RouteScoreBreakdown `json:"score_breakdown,omitempty"`
	AdmissionState  string               `json:"admission_state,omitempty"`
}

// RouteDecisionAttempt contains only safe diagnostics for a dispatched attempt.
// Successful response statuses and time to first token are not inferred.
type RouteDecisionAttempt struct {
	Termination       types.ResponseTerminationKind `json:"termination,omitempty"`
	Diagnostics       []RouteDiagnosticSummary      `json:"response_diagnostics,omitempty"`
	Sequence          int                           `json:"sequence"`
	ChannelID         int                           `json:"channel_id"`
	Success           bool                          `json:"success"`
	ErrorClass        RouteErrorClass               `json:"error_class,omitempty"`
	ErrorHTTPStatus   int                           `json:"error_http_status,omitempty"`
	Origin            AttemptOrigin                 `json:"origin"`
	LatencyMS         int64                         `json:"latency_ms"`
	Acceptance        AttemptAcceptance             `json:"acceptance"`
	RetryPermission   AttemptRetryPermission        `json:"retry_permission"`
	ResponseCommitted bool                          `json:"response_committed,omitempty"`
}

type RouteDiagnosticSummary struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

type RouteDecision struct {
	RetryBudgetExhausted     bool                         `json:"retry_budget_exhausted,omitempty"`
	Event                    string                       `json:"event"`
	RequestID                string                       `json:"request_id"`
	RouteSource              RouteSource                  `json:"route_source"`
	ConfigurationVersion     int64                        `json:"configuration_version,omitempty"`
	RequestModel             string                       `json:"request_model"`
	ActualModel              string                       `json:"actual_model,omitempty"`
	LabSlug                  string                       `json:"lab_slug,omitempty"`
	CatalogVersion           string                       `json:"catalog_version,omitempty"`
	SnapshotVersion          int64                        `json:"snapshot_version,omitempty"`
	ScoringMode              string                       `json:"scoring_mode,omitempty"`
	DynamicScoreApplied      bool                         `json:"dynamic_score_applied"`
	ScoreMetricsUnavailable  bool                         `json:"score_metrics_unavailable,omitempty"`
	StaticPreferredChannelID int                          `json:"static_preferred_channel_id,omitempty"`
	ScoredPreferredChannelID int                          `json:"scored_preferred_channel_id,omitempty"`
	Candidates               []RouteDecisionCandidate     `json:"candidates"`
	Attempts                 []RouteDecisionAttempt       `json:"attempts,omitempty"`
	SelectedChannelID        int                          `json:"selected_channel_id,omitempty"`
	RetryAttempt             int                          `json:"retry_attempt"`
	SameResourceRetry        int                          `json:"same_resource_retry_attempt"`
	FailoverAttempt          int                          `json:"failover_attempt"`
	AdmissionState           string                       `json:"admission_state,omitempty"`
	FinalError               string                       `json:"final_error,omitempty"`
	RejectionOrigin          AttemptOrigin                `json:"rejection_origin,omitempty"`
	GeneratedAt              int64                        `json:"generated_at"`
	ScannedCandidates        int                          `json:"scanned_candidates"`
	UpstreamAttempts         int                          `json:"upstream_attempts"`
	WaitMS                   int64                        `json:"wait_ms"`
	PlanID                   string                       `json:"plan_id,omitempty"`
	ConversionDiagnostics    []types.ConversionDiagnostic `json:"conversion_diagnostics,omitempty"`
	UsageSource              string                       `json:"usage_source,omitempty"`
	TokenBudget              int64                        `json:"token_budget,omitempty"`
	TokenBudgetSource        string                       `json:"token_budget_source,omitempty"`
	HealthEpoch              int64                        `json:"health_epoch,omitempty"`
}

func (decision *RouteDecision) UnmarshalJSON(data []byte) error {
	type current RouteDecision
	value := struct {
		*current
		Legacy string `json:"lease_state"`
	}{current: (*current)(decision)}
	if err := common.Unmarshal(data, &value); err != nil {
		return err
	}
	if decision.AdmissionState == "" {
		decision.AdmissionState = value.Legacy
	}
	return nil
}

func (candidate *RouteDecisionCandidate) UnmarshalJSON(data []byte) error {
	type current RouteDecisionCandidate
	value := struct {
		*current
		Legacy string `json:"lease_state"`
	}{current: (*current)(candidate)}
	if err := common.Unmarshal(data, &value); err != nil {
		return err
	}
	if candidate.AdmissionState == "" {
		candidate.AdmissionState = value.Legacy
	}
	return nil
}

func NewRouteDecision(requestID string, source RouteSource, model string, configurationVersion int64) RouteDecision {
	return RouteDecision{
		Event:                "route_decision",
		RequestID:            requestID,
		RouteSource:          source,
		ConfigurationVersion: configurationVersion,
		RequestModel:         model,
		Candidates:           []RouteDecisionCandidate{},
		GeneratedAt:          time.Now().UnixMilli(),
	}
}

func (d *RouteDecision) SetFinalError(class RouteErrorClass) {
	if d == nil || class == "" {
		return
	}
	// Store the stable class, never provider text that could contain a URL,
	// authorization value, or an upstream response body.
	d.FinalError = string(class)
}
