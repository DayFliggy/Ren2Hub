package model

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
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

// Oversized exponents are rejected before a decimal comparison rescales them
// into a huge integer; the old order spent megabytes on each of these inputs.
func TestChannelRatioRejectsOversizedExponentsCheaply(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, value := range []string{"1e10000000", "1e-10000000", "0e10000000"} {
		_, err := NormalizeChannelRatio(value)
		require.Error(t, err, value)
	}
	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}
