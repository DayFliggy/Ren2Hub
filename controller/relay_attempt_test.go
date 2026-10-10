package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayAttemptClientCancellationPrecedesConversionFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{UpstreamSent: true, UpstreamAccepted: true}
	apiErr := helper.ResponseConversionError(errors.New("conversion stopped"))
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.Equal(t, service.AttemptOriginClientCancel, outcome.Origin)
	assert.Equal(t, service.AttemptAccepted, outcome.Acceptance)
	assert.Equal(t, service.AttemptRetryForbidden, outcome.RetryPermission)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.False(t, classification.MarkChannelModel)
	assert.False(t, classification.Failoverable)
}

// The attempt outcome carries the provider's error type, which the error code
// does not: Anthropic reports a missing model only as type not_found_error.
func TestRelayAttemptOutcomeCarriesProviderErrorType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{UpstreamSent: true}
	response := &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"not_found_error","message":"model: claude-9"}}`))}
	apiErr := service.RelayErrorHandler(context.Background(), response, false)
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.Equal(t, "not_found_error", outcome.ProviderType)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.Equal(t, service.RouteErrorModel, classification.Class)
	assert.True(t, classification.MarkCapability)
	assert.True(t, classification.Failoverable)
}

func TestRelayAttemptDisconnectedWriterIsClientFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{UpstreamSent: true, StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("client closed websocket"))
	apiErr := types.NewOpenAIError(errors.New("stream closed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.NoError(t, c.Request.Context().Err(), "hijacked connections do not cancel the HTTP context")
	assert.Equal(t, service.AttemptOriginClientCancel, outcome.Origin)
	assert.Equal(t, service.AttemptRetryForbidden, outcome.RetryPermission)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.False(t, classification.MarkChannelModel)
	assert.False(t, classification.MarkKey)
	assert.False(t, classification.Failoverable)
}

// An interrupted Responses stream ends with the OpenAI error event shape
// (type, code, message, param, sequence_number), as AxonHub ac7f488 emits it.
func TestWriteRelayStreamFailureResponsesErrorEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("gateway_stream_sequence", 41)
	apiErr := types.NewOpenAIError(errors.New("upstream connection reset"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	writeRelayStreamFailure(c, types.RelayFormatOpenAIResponses, apiErr)

	body := recorder.Body.String()
	require.True(t, strings.HasPrefix(body, "event: error\ndata: "), body)
	require.True(t, strings.HasSuffix(body, "\n\n"), body)
	var event map[string]any
	require.NoError(t, common.UnmarshalJsonStr(strings.TrimSuffix(strings.TrimPrefix(body, "event: error\ndata: "), "\n\n"), &event))
	assert.Equal(t, "error", event["type"])
	assert.Equal(t, "stream_interrupted", event["code"])
	assert.Equal(t, apiErr.MaskSensitiveError(), event["message"])
	require.Contains(t, event, "param")
	assert.Nil(t, event["param"])
	assert.Equal(t, float64(42), event["sequence_number"], "the error follows the last relayed sequence number")
	assert.True(t, c.GetBool("gateway_stream_error_sent"))
}

type failedStreamWriter struct{ gin.ResponseWriter }

func (w *failedStreamWriter) WriteString(string) (int, error) { return 0, io.ErrClosedPipe }

func TestRelayAttemptLegacyCallbackWriteFailureDoesNotPenalizeProvider(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Writer = &failedStreamWriter{ResponseWriter: c.Writer}
	writeErr := helper.StringData(c, `{"type":"message_stop"}`)
	require.ErrorIs(t, writeErr, helper.ErrStreamClientWrite)
	info := &relaycommon.RelayInfo{UpstreamSent: true, StreamStatus: relaycommon.NewStreamStatus()}
	// Legacy scanners may have recorded handler_stop before the controller
	// classifies the retained client error; the first end reason is immutable.
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, writeErr)
	apiErr := types.NewOpenAIError(writeErr, types.ErrorCodeBadResponse, http.StatusBadGateway)
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.Equal(t, service.AttemptOriginClientCancel, outcome.Origin)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.False(t, classification.MarkChannelModel)
	assert.False(t, classification.MarkKey)
	assert.False(t, classification.Failoverable)
}

func TestLocalPricingFailureDoesNotPoisonRouteHealthOrProviderCapacity(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		t.Run(backend, func(t *testing.T) {
			db, _ := setupGatewayAPI(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
			previousDatabaseType := common.MainDatabaseType()
			common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			t.Cleanup(func() { common.SetMainDatabaseType(previousDatabaseType) })
			if backend == "memory" {
				common.RDB, common.RedisEnabled = nil, false
			}
			const channelID = 93001
			initial := model.ChannelHealth{ChannelID: channelID, Model: "pricing-model", State: model.RouteHealthStateClosed, HealthEpoch: 7}
			require.NoError(t, db.Create(&initial).Error)
			for index, tc := range []struct {
				name string
				err  error
				code types.ErrorCode
			}{
				{"tool_price", fmt.Errorf("quote: %w", &billing.PricingError{Code: "missing_price_dimension", Dimension: "tool:web_search"}), "missing_price_dimension"},
				{"usage_contract", &billing.PricingError{Code: "missing_usage_contract", Dimension: "audio_tokens"}, "missing_usage_contract"},
				{"model_missing", errors.New("model price is not configured"), types.ErrorCodeModelPriceError},
				{"invalid_expression", errors.New("invalid model billing expression"), types.ErrorCodeModelPriceError},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := context.Background()
					owner := fmt.Sprintf("pricing-%s-%d", backend, index)
					health, err := service.AcquireRouteAttemptHealth(ctx, channelID, "pricing-model", "pricing-key", owner, time.Minute)
					require.NoError(t, err)
					scopes := []service.GatewayProviderRateScope{{ID: owner, RPM: 1, TPM: 100}}
					rate, err := service.ReserveGatewayProviderRate(ctx, common.RDB, scopes, 90)
					require.NoError(t, err)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceManual})
					c.Set("route_live_health", &routeAttemptHealthState{attempt: &health, rate: &rate})
					info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID}}
					apiErr := helper.BillingPriceAPIError(tc.err)
					apiErr = types.NewError(fmt.Errorf("before send: %w", apiErr), types.ErrorCodeDoRequestFailed)
					apiErr.RetryAfter = time.Minute
					assert.Equal(t, tc.code, apiErr.GetErrorCode())
					assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
					outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
					assert.Equal(t, service.AttemptNotSent, outcome.Acceptance)
					assert.Equal(t, service.AttemptOriginAdmission, outcome.Origin)
					classification := service.ClassifyAttemptOutcome(outcome)
					assert.Equal(t, service.RouteErrorAdmission, classification.Class)
					assert.False(t, classification.MarkKey || classification.MarkCapability || classification.MarkChannelModel)
					assert.False(t, classification.Retryable || classification.Failoverable)
					observeLiveRouteAttempt(c, info, outcome)
					assert.Zero(t, liveRouteRetryCounters(c).TotalAttempts)
					var saved model.ChannelHealth
					require.NoError(t, db.First(&saved, initial.ID).Error)
					assert.Equal(t, model.RouteHealthStateClosed, saved.State)
					assert.Zero(t, saved.FailureCount)
					assert.Zero(t, saved.CooldownUntil)
					assert.EqualValues(t, 7, saved.HealthEpoch)
					nextHealth, err := service.AcquireRouteAttemptHealth(ctx, channelID, "pricing-model", "pricing-key", owner+"-next", time.Minute)
					require.NoError(t, err, "local pricing rejection must leave the next paid request admissible")
					require.NoError(t, service.ReleaseRouteAttemptHealth(ctx, nextHealth))
					nextRate, err := service.ReserveGatewayProviderRate(ctx, common.RDB, scopes, 90)
					require.NoError(t, err, "unsent requests must release RPM/TPM without provider cooldown")
					require.NoError(t, service.FinishGatewayProviderRate(ctx, common.RDB, nextRate, 0, false, false, 0))
				})
			}
		})
	}
}
