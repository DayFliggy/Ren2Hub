package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutePolicyValidateRejectsUnsafeBounds(t *testing.T) {
	policy := RoutePolicy{
		GroupID:             1,
		MaxRatio:            "1000.000000000000000001",
		RetryMode:           RoutePolicyRetryNextChannel,
		MaxFailoverAttempts: 1,
	}
	require.Error(t, policy.Validate())

	policy.MaxRatio = "1"
	policy.MaxFailoverAttempts = RouteMaxFailoverAttempts + 1
	require.Error(t, policy.Validate())
}

func TestRoutePolicyPreservesExactDecimalCeiling(t *testing.T) {
	policy := RoutePolicy{GroupID: 1, MaxRatio: "1.000000000000000001", RetryMode: RoutePolicyRetryNone}
	policy.Normalize()
	require.NoError(t, policy.Validate())
	require.Equal(t, "1.000000000000000001", policy.MaxRatio)
	for _, invalid := range []string{"0", "-1", "NaN", "Inf", "1000.000000000000000001", "0.0000000000000000001"} {
		policy.MaxRatio = invalid
		policy.Normalize()
		require.Error(t, policy.Validate(), invalid)
	}
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
