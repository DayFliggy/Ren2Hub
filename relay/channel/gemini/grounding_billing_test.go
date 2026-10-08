package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Grounding bills the unique non-empty search queries of one response
// (webSearchQueries and imageSearchQueries of every candidate, across every
// stream frame) under google_search, and one grounded prompt when the frozen
// quote prices that key. Ren2Hub seeds no tool prices, so an unpriced
// grounded-prompt key must never reach settlement.
func TestGeminiHandlersCountGroundingQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() {
		constant.StreamingTimeout = oldStreamingTimeout
	})
	const text = `"content":{"role":"model","parts":[{"text":"ok"}]}`
	grounded := func(queries int) map[string]int {
		return map[string]int{dto.BuildInToolGoogleSearch: queries, relaycommon.GoogleSearchGroundedPromptTool: 1}
	}

	tests := []struct {
		name         string
		stream       bool
		promptPriced bool
		frames       []string
		want         map[string]int
	}{
		{
			name:         "native non-stream",
			promptPriced: true,
			frames:       []string{`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["Shanghai weather today","current weather in Shanghai"],"groundingChunks":[{"web":{"uri":"https://a.example"}}]}}]}`},
			want:         grounded(2),
		},
		{
			name:         "duplicates, whitespace and empty queries",
			promptPriced: true,
			frames:       []string{`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["go release",""," go release ","   ","tokyo"]}}]}`},
			want:         grounded(2),
		},
		{
			name:         "union across candidates",
			promptPriced: true,
			frames:       []string{`{"candidates":[{"index":0,` + text + `,"groundingMetadata":{"webSearchQueries":["q1","q2"]}},{"index":1,` + text + `,"groundingMetadata":{"webSearchQueries":["q2","q3"]}}]}`},
			want:         grounded(3),
		},
		{
			name:         "image queries count like web queries",
			promptPriced: true,
			frames:       []string{`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["cat breeds","orange cat"],"imageSearchQueries":["orange tabby cat photo","orange cat"]}}]}`},
			want:         grounded(3),
		},
		{
			name:         "unpriced grounded prompt is not recorded",
			promptPriced: false,
			frames:       []string{`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["q1","q2"]}}]}`},
			want:         map[string]int{dto.BuildInToolGoogleSearch: 2},
		},
		{
			name:         "only empty queries",
			promptPriced: true,
			frames:       []string{`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["","  "],"imageSearchQueries":[""]}}]}`},
			want:         map[string]int{},
		},
		{
			name:         "no grounding metadata",
			promptPriced: true,
			frames:       []string{`{"candidates":[{` + text + `,"finishReason":"STOP"}]}`},
			want:         map[string]int{},
		},
		{
			name:         "stream union across frames",
			stream:       true,
			promptPriced: true,
			frames: []string{
				`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["q1"]}}]}`,
				`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["q2","q1 "]}}]}`,
				`{"candidates":[{` + text + `,"finishReason":"STOP","groundingMetadata":{"webSearchQueries":["q3"]}}]}`,
			},
			want: grounded(3),
		},
		{
			name:         "stream repeats are not added up",
			stream:       true,
			promptPriced: true,
			frames: []string{
				`{"candidates":[{` + text + `,"groundingMetadata":{"webSearchQueries":["q1","q2"]}}]}`,
				`{"candidates":[{` + text + `,"finishReason":"STOP","groundingMetadata":{"webSearchQueries":["q1","q2"]}}]}`,
			},
			want: grounded(2),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.7-flash:generateContent", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: "gemini-3.7-flash",
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3.7-flash"},
			}
			info.PriceData.Quote = groundingQuote(t, info.OriginModelName, tt.promptPriced)
			var apiErr *types.NewAPIError
			if tt.stream {
				body := "data: " + strings.Join(tt.frames, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"
				_, apiErr = geminiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, func(string, *dto.GeminiChatResponse) bool {
					return true
				})
			} else {
				_, apiErr = GeminiTextGenerationHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(tt.frames[0]))})
			}
			require.Nil(t, apiErr)
			counts := map[string]int{}
			if info.ResponsesUsageInfo != nil {
				for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
					counts[name] = tool.CallCount
				}
			}
			assert.Equal(t, tt.want, counts)
		})
	}
}

func groundingQuote(t *testing.T, model string, promptPriced bool) *billing.Quote {
	t.Helper()
	prices := []billing.UnitPrice{{Quantity: "tool:" + dto.BuildInToolGoogleSearch, Unit: "per_thousand_calls", Price: "14"}}
	if promptPriced {
		prices = append(prices, billing.UnitPrice{Quantity: "tool:" + relaycommon.GoogleSearchGroundedPromptTool, Unit: "per_thousand_calls", Price: "35"})
	}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: model, Mode: billing.ModePerCall, Pricing: billing.PricingValues{"ModelPrice": "0"}, Prices: prices}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return quote
}
