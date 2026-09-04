package service

import (
	"image"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestGetImageTokenRejectsOverflowingDimensionArea(t *testing.T) {
	previousMediaToken := constant.GetMediaToken
	previousMediaTokenNotStream := constant.GetMediaTokenNotStream
	constant.GetMediaToken = true
	constant.GetMediaTokenNotStream = true
	t.Cleanup(func() {
		constant.GetMediaToken = previousMediaToken
		constant.GetMediaTokenNotStream = previousMediaTokenNotStream
	})

	width := int(^uint32(0))
	height := int(^uint32(0))
	config := image.Config{Width: width, Height: height}
	source := types.NewBase64FileSource("", "image/heic")
	source.SetCache(&types.CachedFileData{ImageConfig: &config, ImageFormat: "heic"})

	tokens, err := getImageToken(nil, types.NewImageFileMeta(source, "high"), "gpt-4.1-mini", true)
	require.NoError(t, err)
	require.Greater(t, tokens, 0)
	require.LessOrEqual(t, tokens, int(math.Round(1536*1.62)))
}
