package model

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPricingConversionDeclaredEndpoints(t *testing.T) {
	db := modelPricingConfigDB(t)
	for _, tc := range []struct {
		name, endpoints         string
		draft                   PricingValues
		imageCount, unsupported bool
	}{
		{name: "array image token", endpoints: `["image-generation","openai"]`, draft: PricingValues{"ModelRatio": "1", "ImageRatio": "2"}},
		{name: "array image fixed", endpoints: `["image-generation","openai"]`, imageCount: true},
		{name: "padded array image", endpoints: `[" image-generation ","openai"]`, imageCount: true},
		{name: "map image", endpoints: `{"image-generation":{"path":"/v1/images/generations","method":"POST"}}`, imageCount: true},
		{name: "padded map image", endpoints: `{" image-generation ":{"path":"/v1/images/generations","method":"POST"}}`, imageCount: true},
		{name: "array ordinary", endpoints: `["openai","openai-response","openai-response-compact","gemini"]`},
		{name: "empty array", endpoints: `[]`},
		{name: "empty map", endpoints: `{}`},
		{name: "whitespace", endpoints: " \t "},
		{name: "blank map key", endpoints: `{" ":{},"openai":{}}`},
		{name: "array number", endpoints: `["image-generation",1]`, unsupported: true},
		{name: "array null", endpoints: `["image-generation",null]`, unsupported: true},
		{name: "array blank", endpoints: `["image-generation"," "]`, unsupported: true},
		{name: "array object", endpoints: `[{}]`, unsupported: true},
		{name: "null", endpoints: `null`, unsupported: true},
		{name: "scalar", endpoints: `"image-generation"`, unsupported: true},
		{name: "malformed", endpoints: `[`, unsupported: true},
		{name: "padded video", endpoints: `[" openai-video "]`, unsupported: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "endpoint-fixture-" + tc.name
			require.NoError(t, db.Create(&Model{ModelName: name, Endpoints: tc.endpoints, Status: 1, NameRule: NameRuleExact}).Error)
			draft := tc.draft
			if draft == nil {
				draft = PricingValues{"ModelPrice": "0.25"}
			}
			converted, err := PreviewModelPricingConversion(name, draft)
			require.NoError(t, err)
			if tc.unsupported {
				assert.NotEmpty(t, converted.UnsupportedReason)
				assert.Empty(t, converted.Expression)
				return
			}
			require.Empty(t, converted.UnsupportedReason)
			require.NotEmpty(t, converted.Expression)
			assert.Equal(t, tc.imageCount, converted.BillingDetails.ImageCount)
			if tc.draft != nil {
				assert.NotContains(t, converted.Expression, "image_count")
				return
			}
			n := 3
			amount, _, err := billingexpr.RunExprWithRequest(converted.Expression, billingexpr.TokenParams{}, billingexpr.RequestInput{ImageCount: &n})
			require.NoError(t, err)
			want := float64(250000)
			if tc.imageCount {
				want *= float64(n)
			}
			assert.Equal(t, want, amount, "fixed image conversion must preserve the requested quantity")
		})
	}
}
