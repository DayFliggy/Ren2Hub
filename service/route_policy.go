package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrRoutePolicyNotFound         = errors.New("channel route policy is not configured")
	ErrRoutePolicyDisabled         = errors.New("channel route policy is disabled")
	ErrRoutePolicyInvalid          = errors.New("channel route policy is invalid")
	ErrRoutePolicyConflict         = errors.New("channel route policy version conflict")
	ErrRoutePolicyScopeUnavailable = errors.New("route scope concurrency limits are unavailable")
)

func SaveChannelRoutePolicy(input model.ChannelRoutePolicy) (model.ChannelRoutePolicy, error) {
	if model.DB == nil {
		return model.ChannelRoutePolicy{}, ErrRoutePolicyInvalid
	}
	providedVersion := input.Version
	input.Normalize(time.Now())
	if err := input.Validate(); err != nil {
		return model.ChannelRoutePolicy{}, ErrRoutePolicyInvalid
	}
	var saved model.ChannelRoutePolicy
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ChannelRoutePolicy
		err := tx.Where("channel_id = ? AND canonical_model = ?", input.ChannelID, input.CanonicalModel).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.ID != 0 || input.Version != 1 {
				return ErrRoutePolicyConflict
			}
			if err := tx.Create(&input).Error; err != nil {
				return err
			}
			saved = input
			return nil
		}
		if err != nil {
			return err
		}
		if providedVersion <= 0 {
			return ErrRoutePolicyConflict
		}
		if input.ID != 0 && input.ID != current.ID {
			return ErrRoutePolicyConflict
		}
		if input.Version != current.Version {
			return ErrRoutePolicyConflict
		}
		input.ID = current.ID
		input.Version = current.Version + 1
		input.UpdatedAt = time.Now().Unix()
		result := tx.Model(&current).Where("id = ? AND version = ?", current.ID, current.Version).Updates(map[string]any{
			"max_user_concurrency":    input.MaxUserConcurrency,
			"max_token_concurrency":   input.MaxTokenConcurrency,
			"max_channel_concurrency": input.MaxChannelConcurrency,
			"enabled":                 input.Enabled,
			"version":                 input.Version,
			"updated_at":              input.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRoutePolicyConflict
		}
		saved = input
		return nil
	})
	return saved, err
}

// LoadEnabledChannelRoutePolicy is the policy boundary for new routing. It
// never falls back to Channel.capacity_total or another legacy field.
func LoadEnabledChannelRoutePolicy(ctx context.Context, channelID int, requestModel string) (model.ChannelRoutePolicy, error) {
	canonicalModel := modellab.NormalizeModel(requestModel)
	if channelID <= 0 || strings.TrimSpace(canonicalModel) == "" {
		return model.ChannelRoutePolicy{}, ErrRoutePolicyNotFound
	}
	policy, err := model.FindChannelRoutePolicy(ctx, channelID, canonicalModel)
	if errors.Is(err, model.ErrChannelRoutePolicyNotFound) {
		return model.ChannelRoutePolicy{}, ErrRoutePolicyNotFound
	}
	if err != nil {
		return model.ChannelRoutePolicy{}, err
	}
	if !policy.Enabled {
		return model.ChannelRoutePolicy{}, ErrRoutePolicyDisabled
	}
	if err := policy.Validate(); err != nil {
		return model.ChannelRoutePolicy{}, ErrRoutePolicyInvalid
	}
	return policy, nil
}

// BuildRouteLeaseResources maps one enabled policy and the resolved shared
// scope limits to Redis resources. A zero scope limit means no enabled policy
// constrains that shared dimension; a zero on the selected policy alone never
// bypasses a positive limit resolved from another policy.
func BuildRouteLeaseResources(policy model.ChannelRoutePolicy, scopeLimits model.RouteScopeConcurrencyLimits, userID, tokenID int) ([]RouteLeaseResource, error) {
	if err := policy.Validate(); err != nil || !policy.Enabled {
		return nil, ErrRoutePolicyInvalid
	}
	if userID <= 0 || tokenID <= 0 {
		return nil, ErrRoutePolicyInvalid
	}
	if scopeLimits.MaxUserConcurrency < 0 || scopeLimits.MaxUserConcurrency > model.RouteMaxConcurrency ||
		scopeLimits.MaxTokenConcurrency < 0 || scopeLimits.MaxTokenConcurrency > model.RouteMaxConcurrency {
		return nil, ErrRoutePolicyScopeUnavailable
	}
	resources := make([]RouteLeaseResource, 0, 3)
	if scopeLimits.MaxUserConcurrency > 0 {
		resources = append(resources, RouteLeaseResource{Key: UserRouteLeaseKey(userID), Capacity: scopeLimits.MaxUserConcurrency})
	}
	if scopeLimits.MaxTokenConcurrency > 0 {
		resources = append(resources, RouteLeaseResource{Key: TokenRouteLeaseKey(tokenID), Capacity: scopeLimits.MaxTokenConcurrency})
	}
	resources = append(resources, RouteLeaseResource{
		Key:      ChannelModelRouteLeaseKey(policy.ChannelID, policy.CanonicalModel),
		Capacity: policy.MaxChannelConcurrency,
	})
	return resources, nil
}

// AcquireConfiguredRouteLease is the only high-level lease entry point for a
// live route. It requires an explicit policy and a healthy Redis client; no
// capacity field or in-process fallback is consulted.
func AcquireConfiguredRouteLease(ctx context.Context, requestID string, channelID, userID, tokenID int, requestModel string, ttl time.Duration) (RouteLease, model.ChannelRoutePolicy, error) {
	policy, err := LoadEnabledChannelRoutePolicy(ctx, channelID, requestModel)
	if err != nil {
		return RouteLease{}, model.ChannelRoutePolicy{}, err
	}
	if !common.RedisEnabled || common.RDB == nil {
		return RouteLease{}, policy, ErrRouteLeaseUnavailable
	}
	leaseID := uuid.NewString()
	scopeLimits, err := model.FindEnabledRouteScopeConcurrencyLimits(ctx)
	if err != nil {
		return RouteLease{}, policy, fmt.Errorf("%w: %v", ErrRoutePolicyScopeUnavailable, err)
	}
	resources, err := BuildRouteLeaseResources(policy, scopeLimits, userID, tokenID)
	if err != nil {
		return RouteLease{}, policy, err
	}
	lease, err := AcquireRouteLease(ctx, common.RDB, requestID, leaseID, ttl, resources)
	if err != nil {
		return RouteLease{}, policy, err
	}
	lease.ChannelID = channelID
	return lease, policy, nil
}

func ReleaseConfiguredRouteLease(ctx context.Context, lease RouteLease) error {
	if !common.RedisEnabled || common.RDB == nil {
		return ErrRouteLeaseUnavailable
	}
	return ReleaseRouteLease(ctx, common.RDB, lease)
}

// GetRouteRuntimeState reads the mutable facts checked after a lease is
// acquired. Missing health rows mean a never-failed route at epoch one; a
// missing capability snapshot remains version zero and is rejected by the
// caller's expected snapshot comparison.
func GetRouteRuntimeState(ctx context.Context, channelID int, requestModel string) (RouteLeaseRuntimeState, error) {
	if model.DB == nil || channelID <= 0 {
		return RouteLeaseRuntimeState{}, ErrRouteLeaseRuntime
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var channel model.Channel
	if err := model.DB.WithContext(ctx).Select("id", "status").Where("id = ?", channelID).First(&channel).Error; err != nil {
		return RouteLeaseRuntimeState{}, err
	}
	state := RouteLeaseRuntimeState{ChannelEnabled: channel.Status == common.ChannelStatusEnabled, HealthEpoch: 1}
	if fence, err := model.GetChannelCapabilitySnapshotFence(ctx, channelID); err == nil {
		state.CapabilityVersion = fence.ActiveVersion
	} else {
		return RouteLeaseRuntimeState{}, err
	}
	var health model.ChannelHealth
	err := model.DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, modellab.NormalizeModel(requestModel), "").First(&health).Error
	if err == nil {
		state.HealthEpoch = health.HealthEpoch
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return RouteLeaseRuntimeState{}, err
	}
	return state, nil
}

func RouteLiveRoutingEnabled() bool {
	return TokenPrivateRoutingEnabled() && common.GetEnvOrDefaultBool("ROUTE_LIVE_ENABLED", false)
}

// RouteLiveRolloutMatches applies the explicit rollout dimensions after the
// global gates have been checked. Empty allowlists mean "all"; a non-empty
// allowlist is an opt-in boundary for that dimension. This keeps a live route
// rollout scoped without changing the legacy selector or capability defaults.
func RouteLiveRolloutMatches(input LiveRouteRequest) bool {
	if !allowlistMatches("ROUTE_LIVE_USER_IDS", input.UserID) ||
		!allowlistMatches("ROUTE_LIVE_TOKEN_IDS", input.TokenID) ||
		!modelAllowlistMatchesForEnv("ROUTE_LIVE_MODELS", input.RequestModel) {
		return false
	}
	return rolloutStringAllowlistMatches("ROUTE_LIVE_INSTANCES", common.NodeName)
}

func rolloutStringAllowlistMatches(env, value string) bool {
	allowlist := strings.TrimSpace(common.GetEnvOrDefaultString(env, ""))
	if allowlist == "" {
		return true
	}
	value = strings.TrimSpace(value)
	for _, part := range strings.Split(allowlist, ",") {
		if strings.TrimSpace(part) == value {
			return true
		}
	}
	return false
}

// TokenPrivateRoutingEnabled is the capability boundary shared by routing
// APIs, navigation and request middleware. Both the capability and the live
// gate are opt-in; an unset environment never enables private routing.
func TokenPrivateRoutingEnabled() bool {
	return common.GetEnvOrDefaultBool("TOKEN_PRIVATE_ROUTING_ENABLED", false)
}
