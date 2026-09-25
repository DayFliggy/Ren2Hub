package plugins

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactoryPriceRequiresExplicitPriceAndActualUsage(t *testing.T) {
	source, err := Source("alibaba")
	require.NoError(t, err)
	plug, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	_, err = FrozenTaskPriceVersion(plug.Meta, "wan2.6-t2i", "")
	require.ErrorContains(t, err, "no price")
	price, err := FrozenTaskPriceVersion(plug.Meta, "wan2.6-t2i", `u("image_count") * 0.2`)
	require.NoError(t, err)
	quote, err := billing.NewQuoteWithFacts(price, "1", nil, time.Now(), nil)
	require.NoError(t, err)
	_, err = billing.Calculate(quote, billing.Usage{Complete: true, EstimatedFacts: map[string]any{"image_count": 3}})
	require.Error(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{Complete: true, ActualFacts: map[string]any{"image_count": 2}})
	require.NoError(t, err)
	assert.EqualValues(t, 200000, charge.FinalQuota)
}

func TestFactoryPriceDoesNotPriceEstimatedDuration(t *testing.T) {
	source, err := Source("google")
	require.NoError(t, err)
	plug, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	_, err = FrozenTaskPriceVersion(plug.Meta, plug.Meta.Models[0], `u("seconds") * 0.1`)
	require.Error(t, err)
}
