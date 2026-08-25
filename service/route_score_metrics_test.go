package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRouteScoreMetricProvider struct {
	metrics RouteScoreRuntimeMetrics
	err     error
}

func (p testRouteScoreMetricProvider) ResolveRouteScoreMetrics(context.Context, int, string) (RouteScoreRuntimeMetrics, error) {
	return p.metrics, p.err
}

func TestLoadRouteScoreRuntimeMetricsDefaultsToUnknown(t *testing.T) {
	restore := SetRouteScoreMetricProvider(nil)
	t.Cleanup(restore)

	metrics, err := LoadRouteScoreRuntimeMetrics(context.Background(), 1, "gpt-test")
	require.NoError(t, err)
	assert.False(t, metrics.RateLimitKnown)
	assert.False(t, metrics.QuotaKnown)
}

func TestLoadRouteScoreRuntimeMetricsUsesExplicitProvider(t *testing.T) {
	restore := SetRouteScoreMetricProvider(testRouteScoreMetricProvider{metrics: RouteScoreRuntimeMetrics{
		RateLimitHeadroom: 0.25, RateLimitKnown: true,
		QuotaHeadroom: 0.75, QuotaKnown: true,
	}})
	t.Cleanup(restore)

	metrics, err := LoadRouteScoreRuntimeMetrics(context.Background(), 2, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, 0.25, metrics.RateLimitHeadroom)
	assert.True(t, metrics.RateLimitKnown)
	assert.Equal(t, 0.75, metrics.QuotaHeadroom)
	assert.True(t, metrics.QuotaKnown)
}

func TestLoadRouteScoreRuntimeMetricsRejectsProviderFailureOrInvalidValue(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		restore := SetRouteScoreMetricProvider(testRouteScoreMetricProvider{err: errors.New("telemetry unavailable")})
		t.Cleanup(restore)
		_, err := LoadRouteScoreRuntimeMetrics(context.Background(), 3, "gpt-test")
		assert.ErrorIs(t, err, ErrRouteScoreMetricsUnavailable)
	})
	t.Run("invalid value", func(t *testing.T) {
		restore := SetRouteScoreMetricProvider(testRouteScoreMetricProvider{metrics: RouteScoreRuntimeMetrics{
			RateLimitHeadroom: 2, RateLimitKnown: true,
		}})
		t.Cleanup(restore)
		_, err := LoadRouteScoreRuntimeMetrics(context.Background(), 3, "gpt-test")
		assert.ErrorIs(t, err, ErrRouteScoreMetricsUnavailable)
	})
}
