package service

import "time"

type RouteSource string

const (
	RouteSourceLegacy  RouteSource = "legacy"
	RouteSourceManual  RouteSource = "manual"
	RouteSourceAutoLab RouteSource = "auto_lab"
)

type RouteSourceInput struct {
	CapabilityEnabled bool
	HasProfile        bool
	ProfileMode       string
}

// ResolveRouteSource is the only route-mode precedence rule. It is pure so
// middleware, preview, and the future live selector cannot disagree about
// which source owns a request.
func ResolveRouteSource(input RouteSourceInput) RouteSource {
	if !input.CapabilityEnabled || !input.HasProfile {
		return RouteSourceLegacy
	}
	switch input.ProfileMode {
	case "manual":
		return RouteSourceManual
	case "auto_lab":
		return RouteSourceAutoLab
	default:
		return RouteSourceLegacy
	}
}

type RouteDecisionCandidate struct {
	ChannelID       int                  `json:"channel_id"`
	FilterReason    string               `json:"filter_reason,omitempty"`
	Priority        int64                `json:"priority"`
	Position        int                  `json:"position"`
	Weight          int                  `json:"weight"`
	SnapshotVersion int64                `json:"snapshot_version,omitempty"`
	HealthEpoch     int64                `json:"health_epoch,omitempty"`
	CatalogVersion  string               `json:"catalog_version,omitempty"`
	Score           *RouteScoreBreakdown `json:"score_breakdown,omitempty"`
	LeaseState      string               `json:"lease_state,omitempty"`
}

type RouteDecision struct {
	Event                    string                   `json:"event"`
	RequestID                string                   `json:"request_id"`
	RouteSource              RouteSource              `json:"route_source"`
	ConfigurationVersion     int64                    `json:"configuration_version,omitempty"`
	RequestModel             string                   `json:"request_model"`
	ActualModel              string                   `json:"actual_model,omitempty"`
	LabSlug                  string                   `json:"lab_slug,omitempty"`
	CatalogVersion           string                   `json:"catalog_version,omitempty"`
	SnapshotVersion          int64                    `json:"snapshot_version,omitempty"`
	ScoringMode              string                   `json:"scoring_mode,omitempty"`
	DynamicScoreApplied      bool                     `json:"dynamic_score_applied"`
	StaticPreferredChannelID int                      `json:"static_preferred_channel_id,omitempty"`
	ScoredPreferredChannelID int                      `json:"scored_preferred_channel_id,omitempty"`
	Candidates               []RouteDecisionCandidate `json:"candidates"`
	SelectedChannelID        int                      `json:"selected_channel_id,omitempty"`
	RetryAttempt             int                      `json:"retry_attempt"`
	SameResourceRetry        int                      `json:"same_resource_retry_attempt"`
	FailoverAttempt          int                      `json:"failover_attempt"`
	LeaseState               string                   `json:"lease_state,omitempty"`
	FinalError               string                   `json:"final_error,omitempty"`
	GeneratedAt              int64                    `json:"generated_at"`
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
