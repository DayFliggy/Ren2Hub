package common

import (
	"github.com/QuantumNous/new-api/pkg/billing"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountBillableToolCallWebSearchPrefersDeclaredWebSearch(t *testing.T) {
	info := &RelayInfo{
		OriginModelName: "gpt-5.1",
		ResponsesUsageInfo: &ResponsesUsageInfo{
			BuiltInTools: map[string]*BuildInToolInfo{
				dto.BuildInToolWebSearch: {ToolName: dto.BuildInToolWebSearch, CallCount: 0},
			},
		},
	}

	info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearch)
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearchPreview)
}

func TestCountBillableToolCallWebSearchDefaultsToPreview(t *testing.T) {
	info := &RelayInfo{OriginModelName: "gpt-5.1"}

	info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
	require.NotNil(t, info.ResponsesUsageInfo)
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearchPreview)
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestCountBillableToolCallFunctionCallRequiresFrozenPrice(t *testing.T) {
	info := &RelayInfo{OriginModelName: "gpt-5.1"}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "gpt-5.1", Mode: billing.ModePerCall, Pricing: billing.PricingValues{"ModelPrice": "0"}, Prices: []billing.UnitPrice{{Quantity: "tool:my_priced_fn", Unit: "per_call", Price: "0.005"}}}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	info.PriceData.Quote = quote
	info.CountBillableToolCall(dto.BuildInCallFunctionCall, "my_priced_fn")
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "my_priced_fn")
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["my_priced_fn"].CallCount)

	info.CountBillableToolCall(dto.BuildInCallFunctionCall, "unpriced_fn")
	assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "unpriced_fn")
}

// Hosted-tool price keys bill hosted tools only: a client function or tool_use
// with the same name is never counted, even when the frozen quote prices that
// name. An operator price alone reserves nothing.
func TestCountBillableToolCallFunctionCallSkipsReservedNames(t *testing.T) {
	wantBilled := map[string]bool{
		dto.BuildInToolWebSearchPreview: false,
		dto.BuildInToolWebSearch:        false,
		dto.BuildInToolFileSearch:       false,
		dto.BuildInToolGoogleSearch:     false,
		dto.BuildInToolImageGeneration:  false,
		"web_fetch":                     false,
		"code_execution":                false,
		"tool_search":                   false,
		"bing_web_search":               false,
		"x_search_posts":                false,
		"x_search_profiles":             false,
		"search_strategy_agent":         false,
		"lookup_order":                  true,
	}
	var prices []billing.UnitPrice
	for name := range wantBilled {
		prices = append(prices, billing.UnitPrice{Quantity: "tool:" + name, Unit: "per_call", Price: "0.007"})
	}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "gpt-5.1", Mode: billing.ModePerCall, Pricing: billing.PricingValues{"ModelPrice": "0"}, Prices: prices}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)

	for name, billed := range wantBilled {
		info := &RelayInfo{OriginModelName: "gpt-5.1"}
		info.PriceData.Quote = quote
		info.CountBillableToolCall(dto.BuildInCallFunctionCall, name)
		info.CountBillableToolCall(dto.BuildInCallToolUse, name)
		if billed {
			require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, name)
			assert.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[name].CallCount, name)
			continue
		}
		assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, name)
	}
}

// Upstream-reported counts replace per-item counts for the same key; an
// explicit zero silences them and a negative report is ignored.
func TestSetBillableToolCount(t *testing.T) {
	tests := []struct {
		name   string
		record func(info *RelayInfo)
		want   map[string]int
	}{
		{
			name: "report replaces counted items",
			record: func(info *RelayInfo) {
				info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				info.SetBillableToolCount(dto.BuildInToolWebSearchPreview, 5)
			},
			want: map[string]int{dto.BuildInToolWebSearchPreview: 5},
		},
		{
			name: "zero silences counted items",
			record: func(info *RelayInfo) {
				info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
				info.SetBillableToolCount(dto.BuildInToolWebSearchPreview, 0)
			},
			want: map[string]int{dto.BuildInToolWebSearchPreview: 0},
		},
		{
			name: "negative report is ignored",
			record: func(info *RelayInfo) {
				info.SetBillableToolCount(dto.BuildInToolWebSearch, 3)
				info.SetBillableToolCount(dto.BuildInToolWebSearch, -5)
				info.SetBillableToolCount("web_fetch", -1)
			},
			want: map[string]int{dto.BuildInToolWebSearch: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RelayInfo{OriginModelName: "gpt-5.1"}
			tt.record(info)
			assert.Equal(t, tt.want, builtInToolCounts(info))
		})
	}
}

func builtInToolCounts(info *RelayInfo) map[string]int {
	counts := map[string]int{}
	if info.ResponsesUsageInfo == nil {
		return counts
	}
	for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
		counts[name] = tool.CallCount
	}
	return counts
}

func TestImageGenerationCallCounterCompletedOutputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		observe   func(c *ImageGenerationCallCounter)
		wantCount int
	}{
		{
			name: "one final result",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "completed",
					Result: "base64-a",
				}, &idx)
			},
			wantCount: 1,
		},
		{
			name: "two distinct finals",
			observe: func(c *ImageGenerationCallCounter) {
				idx0, idx1 := 0, 1
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Result: "base64-a",
				}, &idx0)
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_2",
					Result: "base64-b",
				}, &idx1)
			},
			wantCount: 2,
		},
		{
			name: "empty result",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Result: "   ",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "failed status",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "failed",
					Result: "base64-a",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "incomplete status",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "incomplete",
					Result: "base64-a",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "cancelled status",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "cancelled",
					Result: "base64-a",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "canceled status",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "canceled",
					Result: "base64-a",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "partial status",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "partial",
					Result: "partial-bytes",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "id dedup",
			observe: func(c *ImageGenerationCallCounter) {
				idx0, idx1 := 0, 1
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					CallId: "call_a",
					Result: "base64-a",
				}, &idx0)
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					CallId: "call_b",
					Result: "base64-b",
				}, &idx1)
			},
			wantCount: 1,
		},
		{
			name: "index dedup",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Result: "base64-a",
				}, &idx)
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_2",
					Result: "base64-b",
				}, &idx)
			},
			wantCount: 1,
		},
		{
			name: "identical artifacts from distinct outputs",
			observe: func(c *ImageGenerationCallCounter) {
				idx0, idx1 := 0, 1
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					Result: "same-bytes",
				}, &idx0)
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					Result: "same-bytes",
				}, &idx1)
			},
			wantCount: 2,
		},
		{
			name: "output_item.done plus completed dedup",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				item := &dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					CallId: "call_1",
					Status: "completed",
					Result: "base64-a",
				}
				c.Observe(item, &idx)
				c.Observe(item, &idx)
			},
			wantCount: 1,
		},
		{
			name: "output_item.done plus incomplete equals zero",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "completed",
					Result: "base64-a",
				}, &idx)
				c.Reset()
			},
			wantCount: 0,
		},
		{
			name: "partial event equals zero",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   "image_generation_call.partial_image",
					ID:     "img_1",
					Result: "partial-bytes",
				}, &idx)
			},
			wantCount: 0,
		},
		{
			name: "in_progress is not completed",
			observe: func(c *ImageGenerationCallCounter) {
				idx := 0
				c.Observe(&dto.ResponsesOutput{
					Type:   dto.ResponsesOutputTypeImageGenerationCall,
					ID:     "img_1",
					Status: "in_progress",
					Result: "base64-a",
				}, &idx)
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			counter := &ImageGenerationCallCounter{}
			tt.observe(counter)
			assert.Equal(t, tt.wantCount, counter.Count())
		})
	}
}

func TestImageGenerationCallCounterRejectsOverLimit(t *testing.T) {
	t.Parallel()

	counter := &ImageGenerationCallCounter{}
	for i := 0; i < dto.MaxImageN+3; i++ {
		idx := i
		counter.Observe(&dto.ResponsesOutput{
			Type:   dto.ResponsesOutputTypeImageGenerationCall,
			ID:     "img_" + strings.Repeat("a", i+1),
			Result: "result-" + strings.Repeat("b", i+1),
		}, &idx)
	}
	require.Equal(t, dto.MaxImageN+3, counter.Count())

	info := &RelayInfo{}
	counter.Commit(info)
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolImageGeneration)
	assert.True(t, info.BillingToolUsageInvalid)
	assert.Equal(t, counter.Count(), info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}

func TestImageGenerationCallCounterCommitDoesNotBillDeclarationsAlone(t *testing.T) {
	t.Parallel()

	info := &RelayInfo{
		ResponsesUsageInfo: &ResponsesUsageInfo{
			BuiltInTools: map[string]*BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					ToolName:  dto.BuildInToolImageGeneration,
					CallCount: 0,
				},
			},
		},
	}
	(&ImageGenerationCallCounter{}).Commit(info)
	assert.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}

func TestIsNonBillableResponsesStatus(t *testing.T) {
	t.Parallel()

	assert.True(t, IsNonBillableResponsesStatus([]byte(`"failed"`)))
	assert.True(t, IsNonBillableResponsesStatus([]byte(`"incomplete"`)))
	assert.True(t, IsNonBillableResponsesStatus([]byte(`"cancelled"`)))
	assert.True(t, IsNonBillableResponsesStatus([]byte(`"canceled"`)))
	assert.False(t, IsNonBillableResponsesStatus([]byte(`"completed"`)))
	assert.False(t, IsNonBillableResponsesStatus(nil))
}
