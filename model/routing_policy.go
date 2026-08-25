package model

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrChannelRoutePolicyNotFound = errors.New("channel route policy not found")

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
