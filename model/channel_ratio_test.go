package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRatioUsesExactDecimalsAndRejectsInvalidStoredValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "NaN", "Inf", "1000.000000000000000001", "1e-19", " 1", "1 "} {
		t.Run(value, func(t *testing.T) {
			_, err := NormalizeChannelRatio(value, false)
			require.Error(t, err)
			_, _, err = (&Channel{CapacityTotal: 1, ChannelRatio: value}).RoutingSettings()
			require.Error(t, err, "routing must not silently default a corrupt stored ratio")
		})
	}
	for _, test := range []struct{ input, expected string }{
		{"", "1"}, {"1.234567890123456789", "1.234567890123456789"}, {"1e-18", "0.000000000000000001"}, {"1000.00", "1000"},
	} {
		value, err := NormalizeChannelRatio(test.input, true)
		require.NoError(t, err)
		assert.Equal(t, test.expected, value)
	}

	db := setupBillingRecoveryTestDB(t)
	channel := Channel{Name: "exact-ratio", Key: "test", ChannelRatio: "1.234567890123456789"}
	require.NoError(t, db.Create(&channel).Error)
	var loaded Channel
	require.NoError(t, db.First(&loaded, channel.Id).Error)
	assert.Equal(t, channel.ChannelRatio, loaded.ChannelRatio)
	raw, err := common.Marshal(loaded)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"channel_ratio":"1.234567890123456789"`)
	var numeric Channel
	require.Error(t, common.UnmarshalJsonStr(`{"channel_ratio":1.5}`, &numeric), "API money multipliers must use decimal strings")
}
