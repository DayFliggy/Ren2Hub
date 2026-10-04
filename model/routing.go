package model

import (
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
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
	ID                      int    `json:"id"`
	GroupID                 int    `json:"group_id" gorm:"uniqueIndex;not null"`
	LoadBalance             bool   `json:"load_balance"`
	MaxRatio                string `json:"max_ratio" gorm:"type:varchar(64);not null;default:1"`
	RetryMode               string `json:"retry_mode" gorm:"type:varchar(32);not null"`
	MaxSameResourceAttempts int    `json:"max_same_resource_attempts" gorm:"not null;default:0"`
	MaxFailoverAttempts     int    `json:"max_failover_attempts" gorm:"not null;default:1"`
	Sticky                  bool   `json:"sticky"`
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

// ChannelHealth carries two distinct kinds of state. State/FailureCount/
// CooldownUntil drive the binary admission gate, while the observation
// counters below feed the ranking bands. FailureCount is consecutive and is
// reset by any success, so it cannot express a rate.
type ChannelHealth struct {
	ID                  int    `json:"id"`
	ChannelID           int    `json:"channel_id" gorm:"uniqueIndex:channel_health_scope;not null"`
	Model               string `json:"model" gorm:"type:varchar(255);uniqueIndex:channel_health_scope;not null"`
	KeyScope            string `json:"key_scope" gorm:"type:varchar(128);uniqueIndex:channel_health_scope;not null"`
	State               string `json:"state" gorm:"type:varchar(32);index;not null"`
	FailureCount        int    `json:"failure_count" gorm:"not null;default:0"`
	CooldownUntil       int64  `json:"cooldown_until" gorm:"bigint"`
	HealthEpoch         int64  `json:"health_epoch" gorm:"not null;default:1"`
	ProbeOwner          string `json:"-" gorm:"type:varchar(128)"`
	ProbeExpiresAt      int64  `json:"-" gorm:"bigint"`
	LastLatencyMS       int64  `json:"last_latency_ms"`
	FirstTokenLatencyMS int64  `json:"first_token_latency_ms"`
	// Cumulative observation counters for ranking. They are monotonic until the
	// row is pruned, so a long-lived healthy channel cannot be reclassified by
	// a single failure, and a channel without the minimum sample stays unknown
	// instead of being scored as perfect.
	ObservedRequests   int64 `json:"observed_requests" gorm:"column:observed_requests;bigint;not null;default:0"`
	ObservedFailures   int64 `json:"observed_failures" gorm:"column:observed_failures;bigint;not null;default:0"`
	TTFTSumMS          int64 `json:"ttft_sum_ms" gorm:"column:ttft_sum_ms;bigint;not null;default:0"`
	TTFTObservationCnt int64 `json:"ttft_observation_count" gorm:"column:ttft_observation_count;bigint;not null;default:0"`
	UpdatedAt          int64 `json:"updated_at" gorm:"bigint;not null"`
}

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
	case RouteModeManual, RouteModeAutoLab:
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
	if p.MaxRatio == "" {
		p.MaxRatio = "1"
	} else if ratio, err := ParseRouteMaxRatio(p.MaxRatio); err == nil {
		p.MaxRatio = ratio.String()
	}
	if p.RetryMode == "" {
		p.RetryMode = RoutePolicyRetryNextChannel
	}
}

func (p *RoutePolicy) Validate() error {
	if _, err := ParseRouteMaxRatio(p.MaxRatio); p.GroupID <= 0 || err != nil {
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

func ParseRouteMaxRatio(value string) (decimal.Decimal, error) {
	if len(value) == 0 || len(value) > 64 {
		return decimal.Zero, errors.New("invalid route price ratio")
	}
	ratio, err := decimal.NewFromString(value)
	if err != nil || !ratio.IsPositive() || ratio.GreaterThan(decimal.NewFromInt(RouteMaxPolicyRatio)) || ratio.Exponent() < -18 || ratio.Exponent() > 18 {
		return decimal.Zero, errors.New("route price ratio must be a decimal string greater than 0 and at most 1000, with at most 18 decimal places")
	}
	return ratio, nil
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
