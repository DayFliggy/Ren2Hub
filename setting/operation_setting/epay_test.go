package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpayMethodsRejectInvalidConfiguration(t *testing.T) {
	for _, input := range []string{
		`[{"name":"","type":"alipay"}]`,
		`[{"name":"Alipay","type":"ali pay"}]`,
		`[{"name":"A","type":"alipay"},{"name":"B","type":" alipay "}]`,
		`[{"name":"A","type":"alipay","min_topup":"0"}]`,
		`[{"name":"A","type":"alipay","min_topup":"-1"}]`,
		`[{"name":"A","type":"alipay","min_topup":"1.5"}]`,
		`[{"name":"A","type":"alipay","min_topup":"9223372036854775807"}]`,
	} {
		_, err := ParseEpayMethods(input)
		require.Error(t, err, input)
	}
	methods, err := ParseEpayMethods(`[{"name":" A ","type":"alipay","min_topup":"5"}]`)
	require.NoError(t, err)
	assert.Equal(t, "A", methods[0]["name"])
	minimum, err := EpayMinimumUSD(10, methods[0])
	require.NoError(t, err)
	assert.EqualValues(t, 10, minimum)
	methods, err = ParseEpayMethods(`[]`)
	require.NoError(t, err)
	assert.Empty(t, methods)
}

func TestEpayPricingRejectsUnusableValues(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "NaN", "Inf", "1e1000", "1e2147483647", "1e-1000", "1e64", "1e-64"} {
		_, err := ParseEpayPrice(raw)
		require.Error(t, err, raw)
	}
	price, err := ParseEpayPrice("7.300000000000000001")
	require.NoError(t, err)
	assert.Equal(t, "7.300000000000000001", price.String())
	for _, raw := range []string{`null`, `[]`, `{"default":0}`, `{"default":-1}`, `{"default":null}`, `{"default":"1"}`, `{"default":1e1000}`, `{"default":1e-1000}`} {
		_, err := ParseEpayGroupRatios(raw)
		require.Error(t, err, raw)
	}
	ratios, err := ParseEpayGroupRatios(`{"default":1.000000000000000001,"vip":0.9}`)
	require.NoError(t, err)
	assert.Equal(t, "1.000000000000000001", ratios["default"].String())
	assert.Equal(t, "0.9", ratios["vip"].String())
	ratios, err = ParseEpayGroupRatios(`{}`)
	require.NoError(t, err)
	assert.Empty(t, ratios)
}
