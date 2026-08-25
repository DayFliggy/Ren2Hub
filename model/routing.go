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
	RouteMaxPolicyWeight            = 1_000_000
	RouteMaxPolicyRatio             = 1_000
	RouteMaxRetryAttempts           = 3
	RouteMaxFailoverAttempts        = 3
	RouteNameMaxLength              = 64
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
	ID               int     `json:"id"`
	ChannelID        int     `json:"channel_id" gorm:"index;uniqueIndex:channel_model_capability_version;not null"`
	RequestModel     string  `json:"request_model" gorm:"type:varchar(255);index;uniqueIndex:channel_model_capability_version;not null"`
	ActualModel      string  `json:"actual_model" gorm:"type:varchar(255);uniqueIndex:channel_model_capability_version;not null"`
	LabSlug          string  `json:"lab_slug" gorm:"type:varchar(64);index"`
	Confidence       float64 `json:"confidence"`
	Source           string  `json:"source" gorm:"type:varchar(64);not null"`
	CatalogVersion   string  `json:"catalog_version" gorm:"type:varchar(128);uniqueIndex:channel_model_capability_version;not null"`
	EndpointTypes    string  `json:"endpoint_types" gorm:"type:text"`
	PathCapabilities string  `json:"path_capabilities" gorm:"type:text"`
	State            string  `json:"state" gorm:"type:varchar(32);index;not null"`
	UpdatedAt        int64   `json:"updated_at" gorm:"bigint;not null"`
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
	ID            int    `json:"id"`
	ChannelID     int    `json:"channel_id" gorm:"uniqueIndex:channel_health_scope;not null"`
	Model         string `json:"model" gorm:"type:varchar(255);uniqueIndex:channel_health_scope;not null"`
	KeyScope      string `json:"key_scope" gorm:"type:varchar(128);uniqueIndex:channel_health_scope;not null"`
	State         string `json:"state" gorm:"type:varchar(32);index;not null"`
	FailureCount  int    `json:"failure_count" gorm:"not null;default:0"`
	CooldownUntil int64  `json:"cooldown_until" gorm:"bigint"`
	HealthEpoch   int64  `json:"health_epoch" gorm:"not null;default:1"`
	LastLatencyMS int64  `json:"last_latency_ms"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;not null"`
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
