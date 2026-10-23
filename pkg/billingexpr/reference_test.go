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
		{`tier("基础", p * 0.123456789012345678 + c * 2 + cr * 0)`, map[string]string{"input": "0.123456789012345678", "output": "2", "cache": "0"}},
		{`tier("free", fixed(0))`, map[string]string{"request": "0"}},
		{`p * 1e-3`, map[string]string{"input": "0.001"}},
	} {
		got, ok := StaticReferencePrices(tc.expression)
		require.True(t, ok, tc.expression)
		require.Equal(t, tc.want, got)
	}
	got, ok := StaticReferencePrices(`p * 1 + c * 2 + cr * 3 + cc * 4 + cc1h * 5 + img * 6 + img_cr * 7 + img_o * 8 + ai * 9 + ao * 10`)
	require.True(t, ok)
	require.Equal(t, map[string]string{
		"input": "1", "output": "2", "cache": "3", "cache_create": "4", "cache_create_1h": "5",
		"image": "6", "image_cache": "7", "image_output": "8", "audio_input": "9", "audio_output": "10",
	}, got)
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
