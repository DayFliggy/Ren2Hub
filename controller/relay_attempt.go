package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type routeAttemptHealthState struct {
	mu      sync.Mutex
	attempt *service.RouteAttemptHealth
	rate    *service.GatewayProviderRateReservation
}

const liveRouteAttemptEvidenceKey = "route_live_attempt_evidence"

func markLiveRouteAdmissionRejection(c *gin.Context, channelID int, err error) bool {
	if !service.LiveRouteQualificationAllowsFailover(err) {
		return false
	}
	reason := service.LiveRouteQualificationReason(err)
	switch {
	case errors.Is(err, service.ErrAdmissionCapacity):
		reason = "capacity_exhausted"
	case errors.Is(err, service.ErrRouteHealthUnavailable):
		reason = "health_unavailable"
	}
	markLiveRouteCandidateFiltered(c, channelID, reason)
	return true
}

// A final reservation can reject a candidate after the advisory scan. No
// upstream work started, so all protocols may continue the existing scan.
func markLiveRoutePreSendRejection(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) bool {
	if apiErr == nil || info.UpstreamSent || relayResponseCommitted(c, info) || requestContextDone(c) || c.GetBool("gateway_retry_stopped") {
		return false
	}
	reason := ""
	switch {
	case errors.Is(apiErr, service.ErrRouteHealthUnavailable):
		reason = "health_unavailable"
	case errors.Is(apiErr, service.ErrGatewayProviderRateLimit):
		reason = "provider_rate_limited"
	default:
		return false
	}
	markLiveRouteCandidateFiltered(c, info.ChannelId, reason)
	return true
}

func scanLiveRouteCandidate(c *gin.Context, channelID int) *types.NewAPIError {
	if err := c.Request.Context().Err(); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusRequestTimeout, types.ErrOptionWithSkipRetry())
	}
	value, _ := c.Get("gateway_scanned_channel_ids")
	scanned, _ := value.(map[int]struct{})
	if _, exists := scanned[channelID]; exists {
		return nil
	}
	if len(scanned) >= service.MaxRouteCandidateChannels {
		return types.NewErrorWithStatusCode(errors.New("route candidate scan limit exceeded"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if scanned == nil {
		scanned = make(map[int]struct{})
		c.Set("gateway_scanned_channel_ids", scanned)
	}
	scanned[channelID] = struct{}{}
	c.Set("gateway_scanned_candidates", len(scanned))
	return nil
}

func queueBusyLiveRouteCandidate(c *gin.Context) (int, bool) {
	if c == nil || c.Request == nil || requestContextDone(c) || c.GetBool("route_queue_selected") {
		return 0, false
	}
	_, budget, available := service.GatewayAdmissionFromContext(c.Request.Context())
	if !available {
		return 0, false
	}
	value, _ := c.Get("route_live_selection")
	selection, ok := value.(service.LiveRouteSelection)
	if !ok {
		return 0, false
	}
	selected := -1
	selectedIdle := false
	var selectedRuntime service.AdmissionSnapshot
	policy, _, policyErr := service.GatewayRoutingPolicy(c.Request.Context())
	canQueue := budget.Remaining() > 0 && (policyErr != nil || policy.ChannelQueueSize > 0)
	for index := range selection.Attempts {
		if !selection.AllowsCandidate(selection.Attempts[index].ChannelID, c.GetInt(liveRoutePreviousChannelKey),
			service.RouteErrorClassification{Retryable: true, Failoverable: true}, liveRouteRetryCounters(c)) {
			continue
		}
		if selection.Attempts[index].FilterReason != "capacity_exhausted" {
			continue
		}
		runtime := service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(selection.Attempts[index].ChannelID))
		// An idle pool may already have been reclaimed since the immediate
		// scan found it busy. Give that candidate a fresh immediate attempt;
		// the final admission remains the only authority to grant its slot.
		idle := runtime.Queued == 0 && (runtime.InFlight == 0 || runtime.InFlight < int64(runtime.Capacity))
		if !idle && (!canQueue || (policyErr == nil && runtime.Queued >= int64(policy.ChannelQueueSize))) {
			continue
		}
		// Reclaimed pools have no retained capacity and zero queue pressure.
		runtime.Capacity = max(runtime.Capacity, 1)
		candidate := selection.Attempts[index]
		if knownRouteCandidateUnavailable(c, candidate) != "" {
			continue
		}
		if selected < 0 || idle && !selectedIdle {
			selected, selectedRuntime, selectedIdle = index, runtime, idle
			continue
		}
		if selectedIdle {
			continue
		}
		first := selection.Attempts[selected]
		sameLayer := selection.Source == service.RouteSourceManual && selection.ManualLoadBalance && candidate.Position == first.Position
		if selection.Decision.DynamicScoreApplied && candidate.Score != nil && first.Score != nil {
			sameLayer = candidate.Priority == first.Priority && candidate.Score.LoadBucket == first.Score.LoadBucket
		}
		if sameLayer && runtime.Queued*int64(selectedRuntime.Capacity) < selectedRuntime.Queued*int64(runtime.Capacity) {
			selected, selectedRuntime = index, runtime
		}
	}
	if selected >= 0 {
		index := selected
		channelID := selection.Attempts[index].ChannelID
		selection.Attempts[index].FilterReason = ""
		for i := range selection.Decision.Candidates {
			if selection.Decision.Candidates[i].ChannelID == channelID {
				selection.Decision.Candidates[i].FilterReason = ""
			}
		}
		c.Set("route_live_selection", selection)
		c.Set("route_queue_selected", true)
		c.Set("route_queue_attempt", index)
		return index, true
	}
	return 0, false
}

func knownRouteCandidateUnavailable(c *gin.Context, candidate service.RouteDecisionCandidate) string {
	if c.Request == nil {
		return ""
	}
	usable, _, healthErr := service.RouteHealthUsable(c.Request.Context(), candidate.ChannelID, candidate.RequestModel, time.Now())
	if healthErr == nil && !usable {
		return "health_unavailable"
	}
	if !common.RedisEnabled {
		return ""
	}
	policy, enabled, policyErr := service.GatewayRoutingPolicy(c.Request.Context())
	if !enabled || policyErr != nil {
		return ""
	}
	rate, rateErr := service.ReadGatewayProviderRateRuntime(c.Request.Context(), common.RDB, service.GatewayChannelRateScopeID(candidate.ChannelID))
	limit := policy.ProviderRates[candidate.ChannelID]
	if rateErr == nil && (rate.CooldownMS > 0 || (limit.RPM > 0 && rate.RPMUsed >= limit.RPM) || (limit.TPM > 0 && rate.TokensUsed+rate.TokensReserved >= limit.TPM)) {
		return "provider_rate_limited"
	}
	// Unknown observations do not claim a quota or bypass the final reservation.
	return ""
}

func (state *routeAttemptHealthState) renew(ctx context.Context) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.attempt == nil {
		return nil
	}
	if err := service.RenewRouteAttemptHealth(ctx, *state.attempt, service.GatewayReservationTTL); err != nil {
		return err
	}
	if state.rate != nil {
		return service.RenewGatewayProviderRate(ctx, common.RDB, *state.rate)
	}
	return nil
}

func prepareLiveRouteSend(c *gin.Context, info *relaycommon.RelayInfo, state *routeAttemptHealthState, owner string, body io.Reader) error {
	if info.UpstreamSent {
		return nil
	}
	if err := c.Request.Context().Err(); err != nil {
		return err
	}
	if c.GetInt(liveRouteAttemptCountKey) >= service.DefaultTotalAttempts {
		return stopLiveRouteRetry(c, errors.New("route send attempt limit exceeded"))
	}
	if err := service.ValidateGatewayChannelIdentity(c, info.ChannelId); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	var retryTicket *service.RetryTicket
	if c.GetInt(liveRouteAttemptCountKey) > 0 {
		var err error
		retryTicket, err = service.ReserveGatewayRetry()
		if err != nil {
			c.Set("gateway_retry_budget_exhausted", true)
			return stopLiveRouteRetry(c, err)
		}
		defer retryTicket.Finish(false)
	}
	if err := prepareRelayOutboundPrice(c, info, body); err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	health, err := service.AcquireRouteAttemptHealth(c.Request.Context(), info.ChannelId,
		info.RouteCapabilityModel, common.GetContextKeyString(c, constant.ContextKeyChannelKey), owner, service.GatewayReservationTTL)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, 503, types.ErrOptionWithSkipRetry())
	}
	state.attempt = &health
	if len(health.Scopes) > 0 {
		c.Set("gateway_health_epoch", health.Scopes[0].Epoch)
	}
	if _, _, enabled := service.GatewayAdmissionFromContext(c.Request.Context()); enabled {
		credential, err := model.EnsureGatewayCredential(c.Request.Context(), info.ChannelId, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, 503, types.ErrOptionWithSkipRetry())
		}
		scopes, err := service.LoadGatewayProviderRateScopes(c.Request.Context(), info.ChannelId, credential.ID)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, 503, types.ErrOptionWithSkipRetry())
		}
		definition, _ := relaycapability.Describe(info.ApiType, info.UpstreamModelName)
		surface, supported := definition.Resolve(info.RelayFormat)
		for _, scope := range scopes {
			if scope.TPM > 0 && (!c.GetBool("gateway_token_budget_known") || !supported || !surface.SupportsTokenBudget()) {
				return types.NewErrorWithStatusCode(errors.New("TPM admission requires a bounded token budget"), types.ErrorCodeInvalidRequest, 400, types.ErrOptionWithSkipRetry())
			}
		}
		reservation, err := service.ReserveGatewayProviderRate(c.Request.Context(), common.RDB, scopes, c.GetInt64("gateway_token_budget"))
		if err != nil {
			status := 503
			if errors.Is(err, service.ErrGatewayProviderRateLimit) {
				status = 429
			}
			return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, status, types.ErrOptionWithSkipRetry())
		}
		state.rate = &reservation
	}
	if info.DeferBillingToSend {
		if err := service.PrepareBillingForSelectedModel(c, info); err != nil {
			return err
		}
	}
	// Cancellation during qualification or billing must not cross the send boundary.
	if err := c.Request.Context().Err(); err != nil {
		return err
	}
	info.RetryIndex = beginLiveRouteUpstreamAttempt(c, info.ChannelId)
	if retryTicket != nil {
		retryTicket.Finish(true)
	} else {
		service.RecordGatewayFirstSend()
	}
	info.UpstreamSent = true
	return nil
}

// Internal protection terminates replay without replacing an actual upstream
// response with a new synthetic provider failure.
func stopLiveRouteRetry(c *gin.Context, cause error) *types.NewAPIError {
	c.Set("gateway_retry_stopped", true)
	if value, ok := c.Get("gateway_last_upstream_error"); ok {
		if previous, valid := value.(*types.NewAPIError); valid && previous != nil {
			result := *previous
			types.ErrOptionWithSkipRetry()(&result)
			return &result
		}
	}
	return types.NewErrorWithStatusCode(cause, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
}

func prepareRelayOutboundPrice(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) error {
	format := info.GetFinalRequestRelayFormat()
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		meta, tokens, err := helper.FinalAudioPriceMeta(info, body)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		c.Set("gateway_token_budget_known", false)
		c.Set("gateway_token_budget", int64(tokens))
		c.Set("gateway_token_budget_source", "audio_input_estimate")
		info.SetEstimatePromptTokens(tokens)
		if info.DeferBillingToSend {
			if _, err := helper.ModelPriceHelper(c, info, tokens, meta); err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
		return nil
	}
	if format == types.RelayFormatOpenAIImage {
		c.Set("gateway_token_budget_known", false)
		c.Set("gateway_token_budget", int64(0))
		c.Set("gateway_token_budget_source", "image_quantity_only")
		meta, err := helper.FinalImagePriceMeta(c, info, body)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if info.DeferBillingToSend {
			if _, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), meta); err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
		return nil
	}
	if body != nil && (format == types.RelayFormatOpenAI || format == types.RelayFormatOpenAIResponses || format == types.RelayFormatOpenAIResponsesCompaction || format == types.RelayFormatClaude || format == types.RelayFormatGemini) {
		data, err := io.ReadAll(body)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		if err := service.ValidateGatewayOutboundState(c, info, data); err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	c.Set("gateway_token_budget_known", false)
	c.Set("gateway_token_budget", int64(0))
	budget, err := helper.AnalyzeOutboundTokenBudget(info, body)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if budget == nil {
		c.Set("gateway_token_budget_source", "wire_budget_unsupported")
		return nil
	}
	c.Set("gateway_token_budget_source", budget.Reason)
	c.Set("gateway_token_budget_known", budget.Bounded)
	c.Set("gateway_token_budget", int64(budget.InputTokens)+int64(budget.OutputTokens))
	if !info.DeferBillingToSend {
		return nil
	}
	if budget.InputKnown {
		info.SetEstimatePromptTokens(budget.InputTokens)
	}
	meta := info.Request.GetTokenCountMeta()
	meta.MaxTokens = budget.OutputTokens
	if _, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), meta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func relayAttemptOutcome(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, started time.Time, sideEffecting bool) service.AttemptOutcome {
	outcome := service.AttemptOutcome{Success: apiErr == nil, Origin: service.AttemptOriginProvider,
		ResponseCommitted: relayResponseCommitted(c, info), LatencyMS: time.Since(started).Milliseconds(),
		Acceptance: service.AttemptNotSent, RetryPermission: service.AttemptRetryPolicy, SideEffecting: sideEffecting}
	if info != nil {
		outcome.SideEffecting = outcome.SideEffecting || info.UpstreamSideEffecting
		if info.UpstreamSent {
			outcome.Acceptance = service.AttemptAcceptanceUnknown
		}
		if info.UpstreamAccepted {
			outcome.Acceptance = service.AttemptAccepted
		}
		if info.UpstreamRejected {
			outcome.Acceptance = service.AttemptRejected
		}
		if info.FirstResponseTime.After(started) {
			outcome.TTFTMS = info.FirstResponseTime.Sub(started).Milliseconds()
		}
	}
	if apiErr == nil {
		return outcome
	}
	if info != nil && info.UpstreamSent {
		c.Set("gateway_last_upstream_error", apiErr)
	}
	outcome.StatusCode, outcome.ProviderCode, outcome.Message, outcome.RetryAfter = apiErr.StatusCode, string(apiErr.GetErrorCode()), apiErr.Error(), apiErr.RetryAfter
	if types.IsSkipRetryError(apiErr) {
		outcome.RetryPermission = service.AttemptRetryForbidden
	}
	if errors.Is(helper.StreamOutputError(c), helper.ErrStreamClientWrite) || (info != nil && info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone) || c.Request.Context().Err() != nil {
		outcome.Origin = service.AttemptOriginClientCancel
		outcome.RetryPermission = service.AttemptRetryForbidden
		return outcome
	}
	var conversionLoss *types.ConversionLossError
	if errors.As(apiErr, &conversionLoss) || apiErr.GetErrorCode() == helper.ResponseConversionErrorCode {
		outcome.Origin = service.AttemptOriginLocalConvert
		outcome.RetryPermission = service.AttemptRetryForbidden
		if conversionLoss != nil {
			c.Set("response_conversion_diagnostics", conversionLoss.Diagnostics)
		}
		return outcome
	}
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeConvertRequestFailed, types.ErrorCodeJsonMarshalFailed, types.ErrorCodeChannelModelMappedError,
		types.ErrorCodeChannelParamOverrideInvalid, types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrorCodeInvalidApiType:
		outcome.Origin = service.AttemptOriginLocalConvert
	case types.ErrorCodeInvalidRequest, types.ErrorCodeReadRequestBodyFailed, types.ErrorCodeBadRequestBody:
		outcome.Origin = service.AttemptOriginLocalParse
	case types.ErrorCodeGetChannelFailed, types.ErrorCodeModelPriceError, types.ErrorCodePreConsumeTokenQuotaFailed,
		types.ErrorCodeInsufficientUserQuota, types.ErrorCodeUpdateDataError:
		outcome.Origin = service.AttemptOriginAdmission
	case types.ErrorCodeDoRequestFailed:
		outcome.Origin = service.AttemptOriginTransport
	case types.ErrorCodeBadResponseStatusCode:
		if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && outcome.Acceptance != service.AttemptAccepted {
			outcome.Acceptance = service.AttemptRejected
		}
	}
	if c.GetBool("gateway_retry_stopped") || liveRouteRenewalFailed(c) || errors.Is(apiErr, service.ErrGatewayStorageUnavailable) || errors.Is(apiErr, service.ErrAdmissionRuntime) || errors.Is(apiErr, service.ErrRouteHealthUnavailable) {
		outcome.Origin = service.AttemptOriginAdmission
	}
	return outcome
}

func observeLiveRouteAttempt(c *gin.Context, info *relaycommon.RelayInfo, outcome service.AttemptOutcome) {
	defer releaseRouteAttempt(c)
	c.Set("gateway_last_attempt_outcome", outcome)
	if routeLiveSelectionActive(c) && info != nil && info.UpstreamSent {
		value, _ := c.Get(liveRouteAttemptEvidenceKey)
		attempts, _ := value.([]service.RouteDecisionAttempt)
		attempt := service.RouteDecisionAttempt{
			Sequence: c.GetInt(liveRouteAttemptCountKey), ChannelID: c.GetInt(liveRoutePreviousChannelKey),
			Success: outcome.Success, Origin: outcome.Origin, LatencyMS: outcome.LatencyMS,
			Acceptance: outcome.Acceptance, RetryPermission: outcome.RetryPermission,
			ResponseCommitted: outcome.ResponseCommitted,
		}
		if value, exists := c.Get("response_termination"); exists {
			if terminal, valid := value.(types.ResponseTermination); valid {
				attempt.Termination = terminal.Kind
			}
		}
		if value, exists := c.Get("response_conversion_diagnostics"); exists {
			if diagnostics, valid := value.([]types.ConversionDiagnostic); valid {
				for _, diagnostic := range diagnostics {
					attempt.Diagnostics = append(attempt.Diagnostics, service.RouteDiagnosticSummary{Code: diagnostic.Code, Path: diagnostic.Path})
				}
			}
		}
		if !outcome.Success {
			attempt.ErrorClass = service.ClassifyAttemptOutcome(outcome).Class
			attempt.ErrorHTTPStatus = outcome.StatusCode
		}
		c.Set(liveRouteAttemptEvidenceKey, append(attempts, attempt))
	}
	stopRouteAttemptRenewal(c)
	value, _ := c.Get("route_live_health")
	state, ok := value.(*routeAttemptHealthState)
	if !ok {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.attempt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if state.rate != nil {
		actualTokens := int64(0)
		usageKnown := false
		if info != nil && info.ResponseUsage != nil {
			actualTokens = int64(info.ResponseUsage.TotalTokens)
			usageKnown = true
		}
		if err := service.FinishGatewayProviderRate(ctx, common.RDB, *state.rate, actualTokens, outcome.Acceptance != service.AttemptNotSent, usageKnown, outcome.RetryAfter); err != nil {
			recordLiveRouteGovernanceFailure(c, state.attempt.ChannelID, "provider_rate_finalize_failed", err)
		}
		state.rate = nil
	}
	if err := service.ObserveRouteAttemptOutcome(ctx, *state.attempt, outcome); err != nil {
		recordLiveRouteGovernanceFailure(c, state.attempt.ChannelID, "health_observation_failed", err)
	}
}

func writeRelayStreamFailure(c *gin.Context, format types.RelayFormat, apiErr *types.NewAPIError) {
	if format == types.RelayFormatOpenAIRealtime {
		return
	}
	errorValue := map[string]any{"message": apiErr.MaskSensitiveError(), "type": "upstream_error", "code": "stream_interrupted"}
	payload := map[string]any{"error": errorValue}
	event := ""
	if format == types.RelayFormatClaude {
		event, payload["type"] = "error", "error"
		errorValue["type"] = "api_error"
	} else if format == types.RelayFormatOpenAIResponses {
		event, payload["type"] = "error", "error"
		payload["message"], payload["code"], payload["sequence_number"] = apiErr.MaskSensitiveError(), "stream_interrupted", c.GetInt("gateway_stream_sequence")+1
	} else if format == types.RelayFormatGemini {
		payload["error"] = map[string]any{"code": http.StatusBadGateway, "message": apiErr.MaskSensitiveError(), "status": "INTERNAL"}
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return
	}
	c.Set("gateway_stream_error_sent", true)
	if event != "" {
		_, _ = fmt.Fprintf(c.Writer, "event: %s\n", event)
	}
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", encoded)
	c.Writer.Flush()
	if format == types.RelayFormatGemini {
		// Google SDKs do not consistently recognize SSE error envelopes. Abort
		// the transport as well so EOF cannot turn a truncated generation into success.
		panic(http.ErrAbortHandler)
	}
}
