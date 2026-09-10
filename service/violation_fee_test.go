package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViolationFeeRejectsNonFiniteAndOverflow(t *testing.T) {
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.MaxFloat64} {
		quota, err := calcViolationFeeQuota(amount, 1)
		require.Error(t, err)
		assert.Zero(t, quota)
	}
	quota, err := calcViolationFeeQuota(0.05, 2)
	require.NoError(t, err)
	assert.Equal(t, common.QuotaRound(0.1*common.QuotaPerUnit), quota)
}
