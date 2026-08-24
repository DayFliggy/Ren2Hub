package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutePolicyValidateRejectsUnsafeBounds(t *testing.T) {
	policy := RoutePolicy{
		GroupID:             1,
		MaxRatio:            RouteMaxPolicyRatio + 1,
		RetryMode:           RoutePolicyRetryNextChannel,
		MaxFailoverAttempts: 1,
	}
	require.Error(t, policy.Validate())

	policy.MaxRatio = 1
	policy.MaxFailoverAttempts = RouteMaxFailoverAttempts + 1
	require.Error(t, policy.Validate())
}

func TestRouteProfileNormalizePreservesExplicitVersion(t *testing.T) {
	profile := UserRouteProfile{UserID: 7, TokenID: 9, Mode: RouteModeManual, Version: 4}
	profile.Normalize(testNow())

	require.Equal(t, int64(4), profile.Version)
	require.Equal(t, RouteProfileStatusEnabled, profile.Status)
	require.NotZero(t, profile.CreatedAt)
	require.Equal(t, profile.CreatedAt, profile.UpdatedAt)
}

func testNow() (nowTime time.Time) {
	return time.Unix(1_700_000_000, 0)
}
