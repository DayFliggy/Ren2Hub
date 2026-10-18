package model

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChannelRatioCanonicalAndInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "NaN", "Inf", "1001", "1e-19", "1e1000000"} {
		_, err := NormalizeChannelRatio(value)
		require.Error(t, err, value)
	}
	value, err := NormalizeChannelRatio("1.2500")
	require.NoError(t, err)
	require.Equal(t, "1.25", value)
	value, err = NormalizeChannelRatio("0.333333333333333333")
	require.NoError(t, err)
	require.Equal(t, "0.333333333333333333", value)
}
