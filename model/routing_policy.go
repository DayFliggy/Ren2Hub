package model

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrChannelRoutePolicyNotFound = errors.New("channel route policy not found")

var ErrRouteScopeConcurrencyUnavailable = errors.New("route scope concurrency limits are unavailable")

// RouteScopeConcurrencyLimits are the effective ceilings for Redis resources
// shared by every new route of one user or token. They deliberately do not
// include the channel/model resource, whose limit remains policy-specific.
type RouteScopeConcurrencyLimits struct {
	MaxUserConcurrency  int
	MaxTokenConcurrency int
}

// FindChannelRoutePolicy returns the policy for one canonical model. A
// missing policy is deliberately distinct from a disabled policy so callers
// can keep the new route fail-closed without changing legacy behavior.
func FindChannelRoutePolicy(ctx context.Context, channelID int, canonicalModel string) (ChannelRoutePolicy, error) {
	if DB == nil || channelID <= 0 || strings.TrimSpace(canonicalModel) == "" {
		return ChannelRoutePolicy{}, ErrChannelRoutePolicyNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var policy ChannelRoutePolicy
	err := DB.WithContext(ctx).
		Where("channel_id = ? AND canonical_model = ?", channelID, canonicalModel).
		First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelRoutePolicy{}, ErrChannelRoutePolicyNotFound
	}
	if err != nil {
		return ChannelRoutePolicy{}, err
	}
	return policy, nil
}

// FindEnabledRouteScopeConcurrencyLimits resolves the single safe capacity for
// the shared user and token lease keys. A non-zero limit on any enabled policy
// constrains every route using that key, so the smallest positive value wins.
// A zero is an unspecified limit, not an exemption from another enabled
// policy's shared-scope ceiling.
func FindEnabledRouteScopeConcurrencyLimits(ctx context.Context) (RouteScopeConcurrencyLimits, error) {
	if DB == nil {
		return RouteScopeConcurrencyLimits{}, ErrRouteScopeConcurrencyUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var policies []ChannelRoutePolicy
	if err := DB.WithContext(ctx).
		Select("max_user_concurrency", "max_token_concurrency").
		Where("enabled = ?", true).
		Find(&policies).Error; err != nil {
		return RouteScopeConcurrencyLimits{}, err
	}

	limits := RouteScopeConcurrencyLimits{}
	for _, policy := range policies {
		if err := limits.include(policy.MaxUserConcurrency, policy.MaxTokenConcurrency); err != nil {
			return RouteScopeConcurrencyLimits{}, err
		}
	}
	return limits, nil
}

func (limits *RouteScopeConcurrencyLimits) include(userLimit, tokenLimit int) error {
	for _, limit := range []int{userLimit, tokenLimit} {
		if limit < 0 || limit > RouteMaxConcurrency {
			return ErrRouteScopeConcurrencyUnavailable
		}
	}
	limits.MaxUserConcurrency = minPositiveRouteConcurrency(limits.MaxUserConcurrency, userLimit)
	limits.MaxTokenConcurrency = minPositiveRouteConcurrency(limits.MaxTokenConcurrency, tokenLimit)
	return nil
}

func minPositiveRouteConcurrency(current, candidate int) int {
	if candidate <= 0 || (current > 0 && current <= candidate) {
		return current
	}
	return candidate
}
