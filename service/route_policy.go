package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/google/uuid"
)

var (
	ErrRoutePolicyNotFound = errors.New("channel route policy is not configured")
	ErrRoutePolicyDisabled = errors.New("channel route policy is disabled")
	ErrRoutePolicyInvalid  = errors.New("channel route policy is invalid")
)

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

// BuildRouteLeaseResources maps one enabled policy to the independent Redis
// resource dimensions. A zero user/token limit means that dimension is not
// bounded; the channel/model limit remains mandatory for an enabled policy.
func BuildRouteLeaseResources(policy model.ChannelRoutePolicy, userID, tokenID int) ([]RouteLeaseResource, error) {
	if err := policy.Validate(); err != nil || !policy.Enabled {
		return nil, ErrRoutePolicyInvalid
	}
	if userID <= 0 || tokenID <= 0 {
		return nil, ErrRoutePolicyInvalid
	}
	resources := make([]RouteLeaseResource, 0, 3)
	if policy.MaxUserConcurrency > 0 {
		resources = append(resources, RouteLeaseResource{Key: UserRouteLeaseKey(userID), Capacity: policy.MaxUserConcurrency})
	}
	if policy.MaxTokenConcurrency > 0 {
		resources = append(resources, RouteLeaseResource{Key: TokenRouteLeaseKey(tokenID), Capacity: policy.MaxTokenConcurrency})
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
	resources, err := BuildRouteLeaseResources(policy, userID, tokenID)
	if err != nil {
		return RouteLease{}, policy, err
	}
	lease, err := AcquireRouteLease(ctx, common.RDB, requestID, leaseID, ttl, resources)
	if err != nil {
		return RouteLease{}, policy, err
	}
	return lease, policy, nil
}
