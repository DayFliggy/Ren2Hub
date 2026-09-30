package billing_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/require"
)

func TestUsageUnitsNeverExchangeDifferentDimensions(t *testing.T) {
	value, err := billing.ConvertUsageUnit("1.25", "minute", "second")
	require.NoError(t, err)
	require.Equal(t, "75", value)
	value, err = billing.ConvertUsageUnit("1", "hour", "second")
	require.NoError(t, err)
	require.Equal(t, "3600", value)
	for _, pair := range [][2]string{{"character", "token"}, {"credit", "second"}, {"image", "count"}} {
		_, err = billing.ConvertUsageUnit("1", pair[0], pair[1])
		require.Error(t, err)
	}
}
