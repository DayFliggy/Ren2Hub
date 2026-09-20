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
