package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTextSettlementRequiresActualTerminalUsage(t *testing.T) {
	for _, test := range []struct {
		name, source                    string
		stream, terminal, success, want bool
	}{
		{"http actual", "provider", false, false, false, true},
		{"http missing", "missing", false, false, false, false},
		{"http invalid", "invalid", false, false, false, false},
		{"http estimate", "estimated_partial", false, false, false, false},
		{"sse actual terminal", "provider", true, true, true, true},
		{"sse early usage", "provider", true, false, false, false},
		{"sse failure cannot borrow done", "provider", true, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}})
			info.IsStream = test.stream
			info.MarkValidOutput()
			if test.terminal {
				kind := types.ResponseFailed
				if test.success {
					kind = types.ResponseCompleted
				}
				ctx.Set("response_termination", types.ResponseTermination{Kind: kind})
				info.StreamStatus = relaycommon.NewStreamStatus()
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			}
			usage := &dto.Usage{PromptTokens: 1, UsageSource: test.source}
			assert.Equal(t, test.want, TextUsageReadyForSettlement(ctx, info, usage))
		})
	}
}
func TestPurePerCallSettlementRequiresTrustedSuccess(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.1"}})
	info.MarkValidOutput()
	assert.True(t, TextUsageReadyForSettlement(ctx, info, nil))
	info.IsStream = true
	assert.False(t, TextUsageReadyForSettlement(ctx, info, nil))
	ctx.Set("response_termination", types.ResponseTermination{Kind: types.ResponseCompleted})
	assert.True(t, TextUsageReadyForSettlement(ctx, info, nil))
}

func TestBinaryAudioPerCallSettlementRequiresCompleteTransport(t *testing.T) {
	for _, reason := range []relaycommon.StreamEndReason{relaycommon.StreamEndReasonDone, relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonClientGone} {
		t.Run(string(reason), func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.1"}})
			info.MarkValidOutput()
			info.StreamStatus = relaycommon.NewStreamStatus()
			info.StreamStatus.SetEndReason(reason, nil)
			assert.Equal(t, reason == relaycommon.StreamEndReasonDone, TextUsageReadyForSettlement(ctx, info, nil))
		})
	}
}
func TestResponsesTerminalZeroIsActualAndPartialCannotHideInSidecar(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test"}}
	a := NewResponsesUsageAccumulator(info)
	a.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "text"})
	a.Observe(&dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Usage: &dto.Usage{}}})
	usage := a.Finish()
	require.NotNil(t, usage)
	assert.Zero(t, usage.CompletionTokens)
	assert.False(t, NormalizedBillingUsage(usage, "openai").Estimated)
	foreign := dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 10, OutputTokens: 1})
	partial := &dto.Usage{UsageSource: "estimated_partial", BillingUsage: foreign}
	facts := NormalizedBillingUsage(partial, "openai")
	assert.True(t, facts.Estimated)
	assert.False(t, facts.Complete)
}

func TestImageCacheUsageRetainsMissingAndExplicitZero(t *testing.T) {
	for _, count := range []*int{nil, new(int), func() *int { v := 7; return &v }()} {
		usage := &dto.Usage{PromptTokens: 10, PromptTokensDetails: dto.InputTokenDetails{CachedTokensDetails: &dto.CachedTokenDetails{ImageTokens: count}}}
		facts := NormalizedBillingUsage(usage, "openai")
		if count == nil {
			assert.Nil(t, facts.ImageCacheReadTokens)
		} else {
			require.NotNil(t, facts.ImageCacheReadTokens)
			assert.EqualValues(t, *count, *facts.ImageCacheReadTokens)
		}
	}
}

func TestImagesWithoutTokenUsageRequireConfirmedCountAndTerminal(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "images", Unit: "per_image", Price: "0.001"}})
	info.MarkValidOutput()
	info.PriceData.Usage = &billing.Usage{Source: "provider_image_count", Complete: true, Quantities: map[string]string{"images": "2"}}
	assert.True(t, TextUsageReadyForSettlement(ctx, info, &dto.Usage{UsageSource: "missing"}))
	info.IsStream = true
	assert.False(t, TextUsageReadyForSettlement(ctx, info, &dto.Usage{UsageSource: "missing"}))
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	assert.True(t, TextUsageReadyForSettlement(ctx, info, &dto.Usage{UsageSource: "missing"}))
	info.PriceData.Usage.Estimated = true
	assert.False(t, TextUsageReadyForSettlement(ctx, info, &dto.Usage{UsageSource: "missing"}))
}

func TestImageExpressionSettlementRequiresEachActualDimension(t *testing.T) {
	for _, expression := range []string{`v1:fixed(0.001) * image_count`, `v1:p * 2 + fixed(0.001) * image_count`} {
		t.Run(expression, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "images", Unit: "per_image", Price: "0.001"}})
			quote, err := billing.NewQuote(billing.PriceVersion{Model: "image", Mode: billing.ModeExpression, Expression: expression}, "1", map[string]any{"quantity": 3}, time.Now())
			require.NoError(t, err)
			info.PriceData.Quote = quote
			info.MarkValidOutput()
			actualTokens := &dto.Usage{PromptTokens: 5, UsageSource: "provider"}
			assert.False(t, TextUsageReadyForSettlement(ctx, info, actualTokens), "request n cannot stand in for an observed image count")
			info.PriceData.Usage = &billing.Usage{Source: "provider_image_count", Complete: true, Quantities: map[string]string{"images": "2"}}
			assert.True(t, TextUsageReadyForSettlement(ctx, info, actualTokens))
			assert.Equal(t, expression == `v1:fixed(0.001) * image_count`, TextUsageReadyForSettlement(ctx, info, &dto.Usage{UsageSource: "missing"}))
			for _, invalid := range []string{"", "0", "1.5", "129"} {
				info.PriceData.Usage.Quantities["images"] = invalid
				assert.False(t, TextUsageReadyForSettlement(ctx, info, actualTokens), invalid)
			}
			info.PriceData.Usage.Quantities["images"] = "2"
			info.PriceData.Usage.Estimated = true
			assert.False(t, TextUsageReadyForSettlement(ctx, info, actualTokens))
		})
	}
}

func TestFlatFeeUsesConfirmedBusinessTerminalAndSettlesOnce(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "sse"}[stream], func(t *testing.T) {
			ctx, info := reservedRealtimeBillingSession(t)
			quote, err := billing.NewQuote(billingTestPublishedPrice(t, billing.PriceVersion{Model: "flat", Mode: billing.ModeUnitPrice, FlatFee: "0.000006"}), "1", nil, time.Now())
			require.NoError(t, err)
			info.PriceData.Quote, info.PriceData.UsePrice, info.IsStream = quote, true, stream
			require.Nil(t, PrepareBillingForSelectedModel(ctx, info))
			info.MarkValidOutput()
			if stream {
				ctx.Set("response_termination", types.ResponseTermination{Kind: types.ResponseCompleted})
			}
			for i := 0; i < 2; i++ {
				PostTextConsumeQuota(ctx, info, &dto.Usage{UsageSource: "missing"}, nil)
				require.NoError(t, info.BillingSettlementError)
			}
			var user model.User
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			assert.EqualValues(t, 3, user.UsedQuota)
			assert.EqualValues(t, 1, user.RequestCount)
			assert.False(t, info.PriceData.Usage.Estimated)
			assert.True(t, info.PriceData.Usage.Complete)
		})
	}
}
