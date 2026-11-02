package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardScopeRejectsMissingIdentity(t *testing.T) {
	for _, scope := range []DashboardScope{{}, DashboardUserScope(0), DashboardUserScope(-1)} {
		_, err := ReadDashboardQuota(context.Background(), scope, 1, 2)
		require.Error(t, err)
		_, err = ReadDashboardLatency(context.Background(), scope, 1, 2)
		require.Error(t, err)
		_, err = ReadDashboardOutcome(context.Background(), scope, 1, 2)
		require.Error(t, err)
	}
	for _, window := range [][2]int64{{0, 1}, {2, 1}, {1, 1}} {
		_, err := ReadDashboardQuota(context.Background(), DashboardSiteScope(), window[0], window[1])
		require.Error(t, err)
		_, err = ReadDashboardLatency(context.Background(), DashboardSiteScope(), window[0], window[1])
		require.Error(t, err)
		_, err = ReadDashboardOutcome(context.Background(), DashboardSiteScope(), window[0], window[1])
		require.Error(t, err)
	}
}
