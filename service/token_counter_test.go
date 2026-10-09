package service

import (
	"image"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
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

// The media switches decide whether an image is measured: with the default
// GET_MEDIA_TOKEN_NOT_STREAM=false a non-stream image assumes a typical size
// even when its bytes are inline, as in NewAPI. A Gemini-native request never
// downloads a URL image to estimate it, whatever the switches say.
func TestEstimateRequestTokenImageMeasurePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousCountToken := constant.CountToken
	previousMediaToken := constant.GetMediaToken
	previousMediaTokenNotStream := constant.GetMediaTokenNotStream
	constant.CountToken, constant.GetMediaToken, constant.GetMediaTokenNotStream = true, true, false
	t.Cleanup(func() {
		constant.CountToken = previousCountToken
		constant.GetMediaToken = previousMediaToken
		constant.GetMediaTokenNotStream = previousMediaTokenNotStream
	})

	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(server.Close)

	inline := func() types.FileSource {
		config := image.Config{Width: 4000, Height: 3000}
		source := types.NewBase64FileSource("inline", "image/png")
		source.SetCache(&types.CachedFileData{ImageConfig: &config, ImageFormat: "png"})
		return source
	}
	for _, tc := range []struct {
		name   string
		format types.RelayFormat
		stream bool
		model  string
		source types.FileSource
		want   int
	}{
		// A measured 4000x3000 image would cost 1568 tokens.
		{"non-stream inline image assumes typical size", types.RelayFormatClaude, false, "claude-sonnet-4-5", inline(), 1373},
		{"stream inline image is measured", types.RelayFormatClaude, true, "claude-sonnet-4-5", inline(), 1568},
		// A downloaded and measured image would cost more than one tile.
		{"gemini-native stream URL image is not downloaded", types.RelayFormatGemini, true, "gemini-2.5-flash", types.NewURLFileSource(server.URL + "/a.png"), 1290},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, tc.model)
			info := &relaycommon.RelayInfo{RelayFormat: tc.format, IsStream: tc.stream}
			meta := &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, Files: []*types.FileMeta{types.NewImageFileMeta(tc.source, "high")}}

			tokens, err := EstimateRequestToken(c, meta, info)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tokens)
		})
	}
	assert.Zero(t, downloads.Load())
}
