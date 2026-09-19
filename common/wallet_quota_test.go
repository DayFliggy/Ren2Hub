package common

import (
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestWalletQuotaDecimalUsesInt64WithoutSaturation(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Quota
	}{{"2147483648", 2147483648}, {"9223372036854775807", Quota(math.MaxInt64)}, {"-9223372036854775808", Quota(math.MinInt64)}, {"500000.99", 500000}} {
		d, err := decimal.NewFromString(tc.input)
		require.NoError(t, err)
		got, err := WalletQuotaFromDecimal(d)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
	for _, input := range []string{"9223372036854775808", "-9223372036854775809"} {
		d, err := decimal.NewFromString(input)
		require.NoError(t, err)
		_, err = WalletQuotaFromDecimal(d)
		assert.ErrorIs(t, err, ErrQuotaOverflow)
	}
}
