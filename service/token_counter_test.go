package service

import (
	"image"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HEIF dimensions are uint32 values whose product overflows int. The image
// estimate must stay positive and bounded by each provider's resize rule.
func TestGetImageTokenBoundsOverflowingDimensions(t *testing.T) {
	previousMediaToken := constant.GetMediaToken
	previousMediaTokenNotStream := constant.GetMediaTokenNotStream
	constant.GetMediaToken = true
	constant.GetMediaTokenNotStream = true
	t.Cleanup(func() {
		constant.GetMediaToken = previousMediaToken
		constant.GetMediaTokenNotStream = previousMediaTokenNotStream
	})

	side := int(^uint32(0))
	for _, tc := range []struct {
		model string
		want  int
	}{
		// Fit within 2048x2048: 64x64 patches x 1.62.
		{"gpt-4.1-mini", 6636},
		// Claude 4.7 high-resolution tier: 69x69 patches plus 3 framing tokens.
		{"claude-opus-4-7", 4764},
		// Gemini 2.x: 12 tiles plus one thumbnail tile.
		{"gemini-2.5-flash", 3354},
	} {
		t.Run(tc.model, func(t *testing.T) {
			config := image.Config{Width: side, Height: side}
			source := types.NewBase64FileSource("", "image/heic")
			source.SetCache(&types.CachedFileData{ImageConfig: &config, ImageFormat: "heic"})

			tokens, err := getImageToken(nil, types.NewImageFileMeta(source, "high"), tc.model, true)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
}
