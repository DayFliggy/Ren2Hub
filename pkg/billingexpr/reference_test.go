package billingexpr

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStaticReferencePricesPreserveLiteralsAndUnknowns(t *testing.T) {
	for _, tc := range []struct {
		expression string
		want       map[string]string
	}{
		{`tier("基础", p * 0.123456789012345678 + c * 2 + cr * 0)`, map[string]string{"input": "0.123456789012345678", "output": "2"}},
		{`tier("free", fixed(0))`, map[string]string{"request": "0"}},
		{`p * 1e-3`, map[string]string{"input": "0.001"}},
	} {
		got, ok := StaticReferencePrices(tc.expression)
		require.True(t, ok, tc.expression)
		require.Equal(t, tc.want, got)
	}
	for _, expression := range []string{
		`tier("base", p * 1) * (param("fast") == true ? 2 : 1)`,
		`len < 100 ? tier("small", p * 1) : tier("large", p * 2)`,
		`tier("image", fixed(0.01)) * image_count`,
		`p * c`, `p * (1 + 2)`, `p * 1 + 4`,
	} {
		_, ok := StaticReferencePrices(expression)
		require.False(t, ok, expression)
	}
}
