package model

import (
	"errors"
	"strings"
	"time"
)

const (
	RouteModeLegacy  = "legacy"
	RouteModeManual  = "manual"
	RouteModeAutoLab = "auto_lab"

	RouteGroupKindManual  = "manual"
	RouteGroupKindAutoLab = "auto_lab"

	RouteSourcePlatform = "platform"
	RouteSourceMarket   = "market"

	RoutePolicyRetryNone            = "none"
	RoutePolicyRetrySameChannel     = "same_channel"
	RoutePolicyRetryNextChannel     = "next_channel"
	RoutePolicyRetrySameThenNext    = "same_then_next"
	RouteProfileStatusEnabled       = 1
	RouteProfileStatusDisabled      = 2
	RouteEntitlementStatusEnabled   = 1
	RouteEntitlementStatusRevoked   = 2
	RouteHealthStateClosed          = "closed"
	RouteHealthStateOpen            = "open"
	RouteHealthStateHalfOpen        = "half_open"
	RouteCapabilityStateEligible    = "eligible"
	RouteCapabilityStateUnresolved  = "unresolved"
	RouteCapabilityStateUnsupported = "unsupported"
	RouteCapabilityStateDisabled    = "disabled"
	RouteCapabilityStateConflict    = "conflict"
	RouteCapabilityRefreshActive    = "active"
	RouteCapabilityRefreshBuilding  = "building"
	RouteCapabilityRefreshFailed    = "failed"
	ChannelCapabilityProjectionV1   = 1
	RouteShadowObservationGlobal    = "global"
	RouteShadowObservationModel     = "model"
	RouteMaxPolicyWeight            = 1_000_000
	RouteMaxPolicyRatio             = 1_000
	RouteMaxRetryAttempts           = 3
	RouteMaxFailoverAttempts        = 3
	RouteNameMaxLength              = 64
	RouteMaxConcurrency             = 1_000_000
)

type UserRouteProfile struct {
	ID            int    `json:"id"`
	UserID        int    `json:"user_id" gorm:"index;not null"`
	TokenID       int    `json:"token_id" gorm:"uniqueIndex;not null"`
	Mode          string `json:"mode" gorm:"type:varchar(32);index;not null"`
	ActiveGroupID *int   `json:"active_group_id" gorm:"index"`
	Version       int64  `json:"version" gorm:"not null;default:1"`
	Status        int    `json:"status" gorm:"not null;default:1"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;not null"`
}

type UserRouteGroup struct {
	ID        int    `json:"id"`
	ProfileID int    `json:"profile_id" gorm:"index;not null"`
	Name      string `json:"name" gorm:"type:varchar(64);not null"`
	Kind      string `json:"kind" gorm:"type:varchar(32);not null"`
	Enabled   bool   `json:"enabled"`
	Position  int    `json:"position" gorm:"not null;default:0"`
}

type UserRouteEntry struct {
	ID        int    `json:"id"`
	GroupID   int    `json:"group_id" gorm:"index;not null"`
	ChannelID int    `json:"channel_id" gorm:"index;not null"`
	Source    string `json:"source" gorm:"type:varchar(32);not null"`
	Enabled   bool   `json:"enabled"`
	Position  int    `json:"position" gorm:"not null;default:0"`
	Weight    int    `json:"weight" gorm:"not null;default:0"`
}

type RoutePolicy struct {
	ID                      int     `json:"id"`
	GroupID                 int     `json:"group_id" gorm:"uniqueIndex;not null"`
	LoadBalance             bool    `json:"load_balance"`
	MaxRatio                float64 `json:"max_ratio" gorm:"not null;default:1"`
	RetryMode               string  `json:"retry_mode" gorm:"type:varchar(32);not null"`
	MaxSameResourceAttempts int     `json:"max_same_resource_attempts" gorm:"not null;default:0"`
	MaxFailoverAttempts     int     `json:"max_failover_attempts" gorm:"not null;default:1"`
	Sticky                  bool    `json:"sticky"`
}

type ChannelModelCapability struct {
	ID                int     `json:"id"`
	ChannelID         int     `json:"channel_id" gorm:"index;uniqueIndex:channel_model_capability_snapshot;not null"`
	SnapshotVersion   int64   `json:"snapshot_version" gorm:"index;uniqueIndex:channel_model_capability_snapshot;not null;default:0"`
	RequestModel      string  `json:"request_model" gorm:"type:varchar(255);index;uniqueIndex:channel_model_capability_snapshot;not null"`
	ActualModel       string  `json:"actual_model" gorm:"type:varchar(255);not null"`
	LabSlug           string  `json:"lab_slug" gorm:"type:varchar(64);index"`
	Confidence        float64 `json:"confidence"`
	Source            string  `json:"source" gorm:"type:varchar(64);not null"`
	CatalogVersion    string  `json:"catalog_version" gorm:"type:varchar(128);not null"`
	SourceHash        string  `json:"source_hash" gorm:"type:varchar(128);index;not null;default:''"`
	AbilityGroups     string  `json:"ability_groups" gorm:"type:text"`
	EndpointTypes     string  `json:"endpoint_types" gorm:"type:text"`
	PathCapabilities  string  `json:"path_capabilities" gorm:"type:text"`
	ChannelStatus     int     `json:"channel_status" gorm:"index"`
	Priority          int64   `json:"priority"`
	Weight            int     `json:"weight"`
	ChannelType       int     `json:"channel_type"`
	ProjectionVersion int     `json:"projection_version" gorm:"not null;default:0"`
	IsMixed           bool    `json:"is_mixed"`
	State             string  `json:"state" gorm:"type:varchar(32);index;not null"`
	UpdatedAt         int64   `json:"updated_at" gorm:"bigint;not null"`
}

// ChannelCapabilitySnapshot fences a channel's immutable capability rows.
// Only ActiveVersion participates in request-time routing; older versions are
// retained briefly for diagnosis and deterministic decision replay.
type ChannelCapabilitySnapshot struct {
	ID                       int    `json:"id"`
	ChannelID                int    `json:"channel_id" gorm:"uniqueIndex;not null"`
	ActiveVersion            int64  `json:"active_version" gorm:"not null;default:0"`
	CatalogVersion           string `json:"catalog_version" gorm:"type:varchar(128);not null"`
	SourceHash               string `json:"source_hash" gorm:"type:varchar(128);not null"`
	RefreshStatus            string `json:"refresh_status" gorm:"type:varchar(32);index;not null"`
	RefreshedAt              int64  `json:"refreshed_at" gorm:"bigint;not null"`
	LastFailedSourceHash     string `json:"last_failed_source_hash,omitempty" gorm:"type:varchar(128)"`
	LastFailedCatalogVersion string `json:"last_failed_catalog_version,omitempty" gorm:"type:varchar(128)"`
	LastFailedAt             int64  `json:"last_failed_at,omitempty" gorm:"bigint"`
	UpdatedAt                int64  `json:"updated_at" gorm:"bigint;not null"`
}

type UserChannelEntitlement struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"uniqueIndex:user_channel_entitlement;not null"`
	ChannelID int    `json:"channel_id" gorm:"uniqueIndex:user_channel_entitlement;not null"`
	Source    string `json:"source" gorm:"type:varchar(32);uniqueIndex:user_channel_entitlement;not null"`
	Status    int    `json:"status" gorm:"not null;default:1"`
	ExpiresAt int64  `json:"expires_at" gorm:"bigint"`
	RevokedAt int64  `json:"revoked_at" gorm:"bigint"`
	Reason    string `json:"reason" gorm:"type:varchar(255)"`
}

type ChannelHealth struct {
	ID                  int    `json:"id"`
	ChannelID           int    `json:"channel_id" gorm:"uniqueIndex:channel_health_scope;not null"`
	Model               string `json:"model" gorm:"type:varchar(255);uniqueIndex:channel_health_scope;not null"`
	KeyScope            string `json:"key_scope" gorm:"type:varchar(128);uniqueIndex:channel_health_scope;not null"`
	State               string `json:"state" gorm:"type:varchar(32);index;not null"`
	FailureCount        int    `json:"failure_count" gorm:"not null;default:0"`
	CooldownUntil       int64  `json:"cooldown_until" gorm:"bigint"`
	HealthEpoch         int64  `json:"health_epoch" gorm:"not null;default:1"`
	LastLatencyMS       int64  `json:"last_latency_ms"`
	FirstTokenLatencyMS int64  `json:"first_token_latency_ms"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint;not null"`
}

// ChannelRoutePolicy is the configuration source for distributed admission
// control. It is intentionally separate from Channel.capacity_total and
// Channel.capacity_used, which remain operational/display fields. User and
// token limits use shared Redis keys, so their effective value is the smallest
// positive limit across enabled policies; channel/model limits stay local to
// this policy.
type ChannelRoutePolicy struct {
	ID                    int    `json:"id"`
	ChannelID             int    `json:"channel_id" gorm:"uniqueIndex:channel_route_policy_model;not null"`
	CanonicalModel        string `json:"canonical_model" gorm:"type:varchar(255);uniqueIndex:channel_route_policy_model;not null"`
	MaxUserConcurrency    int    `json:"max_user_concurrency" gorm:"not null;default:0"`
	MaxTokenConcurrency   int    `json:"max_token_concurrency" gorm:"not null;default:0"`
	MaxChannelConcurrency int    `json:"max_channel_concurrency" gorm:"not null;default:0"`
	Enabled               bool   `json:"enabled"`
	Version               int64  `json:"version" gorm:"not null;default:1"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint;not null"`
}

// RouteShadowHourlyObservation stores only low-cardinality Shadow acceptance
// counters. It deliberately excludes request, user, token, channel, and
// credential identifiers; InstanceID is a boot-scoped UUID.
type RouteShadowHourlyObservation struct {
	ID                     int    `json:"id"`
	HourStart              int64  `json:"hour_start" gorm:"uniqueIndex:route_shadow_hourly_observation;not null"`
	InstanceID             string `json:"instance_id" gorm:"type:varchar(96);uniqueIndex:route_shadow_hourly_observation;not null"`
	Scope                  string `json:"scope" gorm:"type:varchar(16);uniqueIndex:route_shadow_hourly_observation;not null"`
	ModelName              string `json:"model_name" gorm:"type:varchar(255);uniqueIndex:route_shadow_hourly_observation;not null;default:''"`
	SealedAt               int64  `json:"sealed_at" gorm:"bigint;not null;default:0"`
	DataLossPossible       bool   `json:"data_loss_possible" gorm:"not null;default:false"`
	ShadowDecisions        int64  `json:"shadow_decisions" gorm:"not null;default:0"`
	ShadowInitialDecisions int64  `json:"shadow_initial_decisions" gorm:"not null;default:0"`
	ShadowDiffs            int64  `json:"shadow_diffs" gorm:"not null;default:0"`
	CapabilityResolved     int64  `json:"capability_resolved" gorm:"not null;default:0"`
	CapabilityUnresolved   int64  `json:"capability_unresolved" gorm:"not null;default:0"`
	MappingConflict        int64  `json:"mapping_conflict" gorm:"not null;default:0"`
	UnknownFiltered        int64  `json:"unknown_filtered" gorm:"not null;default:0"`
	UnknownAdmitted        int64  `json:"unknown_admitted" gorm:"not null;default:0"`
	MixedDecisions         int64  `json:"mixed_decisions" gorm:"not null;default:0"`
	UnauthorizedFiltered   int64  `json:"unauthorized_filtered" gorm:"not null;default:0"`
	UnauthorizedAdmitted   int64  `json:"unauthorized_admitted" gorm:"not null;default:0"`
	SnapshotStale          int64  `json:"snapshot_stale" gorm:"not null;default:0"`
	EventAttempted         int64  `json:"event_attempted" gorm:"not null;default:0"`
	EventEnqueued          int64  `json:"event_enqueued" gorm:"not null;default:0"`
	EventDropped           int64  `json:"event_dropped" gorm:"not null;default:0"`
	EventEncodeFailed      int64  `json:"event_encode_failed" gorm:"not null;default:0"`
	EventSubmitted         int64  `json:"event_submitted" gorm:"not null;default:0"`
	EventWriteFailed       int64  `json:"event_write_failed" gorm:"not null;default:0"`
	RefreshSuccess         int64  `json:"refresh_success" gorm:"not null;default:0"`
	RefreshFailure         int64  `json:"refresh_failure" gorm:"not null;default:0"`
	SnapshotConflict       int64  `json:"snapshot_conflict" gorm:"not null;default:0"`
	RefreshLagCount        int64  `json:"refresh_lag_count" gorm:"not null;default:0"`
	RefreshLagLE1S         int64  `json:"refresh_lag_le_1s" gorm:"column:refresh_lag_le_1s;not null;default:0"`
	RefreshLagLE5S         int64  `json:"refresh_lag_le_5s" gorm:"column:refresh_lag_le_5s;not null;default:0"`
	RefreshLagLE15S        int64  `json:"refresh_lag_le_15s" gorm:"column:refresh_lag_le_15s;not null;default:0"`
	RefreshLagLE30S        int64  `json:"refresh_lag_le_30s" gorm:"column:refresh_lag_le_30s;not null;default:0"`
	RefreshLagLE60S        int64  `json:"refresh_lag_le_60s" gorm:"column:refresh_lag_le_60s;not null;default:0"`
	RefreshLagLE120S       int64  `json:"refresh_lag_le_120s" gorm:"column:refresh_lag_le_120s;not null;default:0"`
	RefreshLagLE300S       int64  `json:"refresh_lag_le_300s" gorm:"column:refresh_lag_le_300s;not null;default:0"`
	RefreshLagGT300S       int64  `json:"refresh_lag_gt_300s" gorm:"column:refresh_lag_gt_300s;not null;default:0"`
	UpdatedAt              int64  `json:"updated_at" gorm:"bigint;not null"`
}

var (
	ErrInvalidChannelRoutePolicy = errors.New("invalid channel route policy")
)

func (p *UserRouteProfile) Normalize(now time.Time) {
	if p.Mode == "" {
		p.Mode = RouteModeManual
	}
	if p.Version <= 0 {
		p.Version = 1
	}
	if p.Status == 0 {
		p.Status = RouteProfileStatusEnabled
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = now.Unix()
	}
	p.UpdatedAt = now.Unix()
}

func (p *UserRouteProfile) Validate() error {
	if p.UserID <= 0 || p.TokenID <= 0 {
		return errors.New("route profile owner and token are required")
	}
	switch p.Mode {
	case RouteModeLegacy, RouteModeManual, RouteModeAutoLab:
		return nil
	default:
		return errors.New("invalid route profile mode")
	}
}

func (g *UserRouteGroup) Validate() error {
	if g.ProfileID <= 0 || strings.TrimSpace(g.Name) == "" {
		return errors.New("route group profile and name are required")
	}
	if len([]rune(strings.TrimSpace(g.Name))) > RouteNameMaxLength {
		return errors.New("route group name is too long")
	}
	if g.Kind != RouteGroupKindManual {
		return errors.New("user route groups must be manual")
	}
	if g.Position < 0 {
		return errors.New("route group position cannot be negative")
	}
	return nil
}

func (e *UserRouteEntry) Validate() error {
	if e.GroupID <= 0 || e.ChannelID <= 0 {
		return errors.New("route entry group and channel are required")
	}
	if e.Source != RouteSourcePlatform {
		return errors.New("market route entries are not enabled")
	}
	if e.Position < 0 || e.Weight < 0 || e.Weight > RouteMaxPolicyWeight {
		return errors.New("invalid route entry position or weight")
	}
	return nil
}

func (p *RoutePolicy) Normalize() {
	if p.MaxRatio == 0 {
		p.MaxRatio = 1
	}
	if p.RetryMode == "" {
		p.RetryMode = RoutePolicyRetryNextChannel
	}
}

func (p *RoutePolicy) Validate() error {
	if p.GroupID <= 0 || p.MaxRatio <= 0 || p.MaxRatio > RouteMaxPolicyRatio {
		return errors.New("invalid route policy group or ratio")
	}
	switch p.RetryMode {
	case RoutePolicyRetryNone, RoutePolicyRetrySameChannel, RoutePolicyRetryNextChannel, RoutePolicyRetrySameThenNext:
	default:
		return errors.New("invalid route policy retry mode")
	}
	if p.MaxSameResourceAttempts < 0 || p.MaxSameResourceAttempts > RouteMaxRetryAttempts ||
		p.MaxFailoverAttempts < 0 || p.MaxFailoverAttempts > RouteMaxFailoverAttempts {
		return errors.New("route policy retry attempts exceed limits")
	}
	return nil
}

func (c *ChannelModelCapability) Normalize(now time.Time) {
	if c.State == "" {
		c.State = RouteCapabilityStateUnresolved
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = now.Unix()
	}
}

func (s *ChannelCapabilitySnapshot) Normalize(now time.Time) {
	if s.RefreshStatus == "" {
		s.RefreshStatus = RouteCapabilityRefreshActive
	}
	if s.RefreshedAt == 0 {
		s.RefreshedAt = now.Unix()
	}
	if s.UpdatedAt == 0 {
		s.UpdatedAt = now.Unix()
	}
}

func (h *ChannelHealth) Normalize(now time.Time) {
	if h.State == "" {
		h.State = RouteHealthStateClosed
	}
	if h.HealthEpoch <= 0 {
		h.HealthEpoch = 1
	}
	if h.UpdatedAt == 0 {
		h.UpdatedAt = now.Unix()
	}
}

func (p *ChannelRoutePolicy) Normalize(now time.Time) {
	if p.Version <= 0 {
		p.Version = 1
	}
	p.CanonicalModel = strings.TrimSpace(p.CanonicalModel)
	p.UpdatedAt = now.Unix()
}

func (p ChannelRoutePolicy) Validate() error {
	if p.ChannelID <= 0 || strings.TrimSpace(p.CanonicalModel) == "" {
		return ErrInvalidChannelRoutePolicy
	}
	if p.Version <= 0 {
		return ErrInvalidChannelRoutePolicy
	}
	for _, capacity := range []int{p.MaxUserConcurrency, p.MaxTokenConcurrency, p.MaxChannelConcurrency} {
		if capacity < 0 || capacity > RouteMaxConcurrency {
			return ErrInvalidChannelRoutePolicy
		}
	}
	if p.Enabled && p.MaxChannelConcurrency <= 0 {
		return ErrInvalidChannelRoutePolicy
	}
	return nil
}
