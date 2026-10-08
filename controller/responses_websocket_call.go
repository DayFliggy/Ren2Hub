package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func (s *responsesWSSession) runCall(c *gin.Context, state *responsesWSCallState, create responsesWSCreateRequest) (apiErr *types.NewAPIError) {
	started := time.Now()
	var info *relaycommon.RelayInfo
	defer func() {
		if info != nil {
			// Same rule as HTTP relay: delivered output is billed, a failed call
			// without output is released.
			completedNormally := apiErr == nil && info.StreamStatus.IsNormalEnd()
			if info.ResponseUsage != nil && (completedNormally || info.HasValidOutput()) {
				service.PostTextConsumeQuota(c, info, info.ResponseUsage, nil)
			} else if info.Billing != nil {
				info.Billing.Refund(c)
			}
			observeLiveRouteAttempt(c, info, relayAttemptOutcome(c, info, apiErr, started, false))
		}
		releaseRouteAttempt(c)
		finalizeLiveRouteDecision(c, info, apiErr)
		if apiErr != nil || (info != nil && info.StreamStatus != nil && !info.StreamStatus.IsNormalEnd()) {
			middleware.MarkRelayRequestFailed(c)
		}
	}()
	request, err := helper.GetAndValidateRequest(c, types.RelayFormatOpenAIResponses)
	if err != nil {
		return newResponsesWSInvalidRequestError(err)
	}
	if create.Request.Model == "" {
		return newResponsesWSInvalidRequestError(errors.New("model is required"))
	}
	if err := service.TokenModelPermissionError(c, create.Request.Model); err != nil {
		return err
	}
	userID, tokenID := c.GetInt("id"), common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	if s.lockedChannelID != 0 && (s.lockedUserID != userID || s.lockedTokenID != tokenID) {
		state.closeAfter = true
		return newResponsesWSInvalidRequestError(errors.New("websocket identity changed; reconnect required"))
	}
	common.SetContextKey(c, constant.ContextKeyOriginalModel, create.Request.Model)
	common.SetContextKey(c, constant.ContextKeyResponsesNativeRequired, true)
	c.Set("responses_websocket_required", true)
	bound, err := service.ResolveGatewayRequestState(c)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, service.GatewayStateHTTPStatus(err), types.ErrOptionWithSkipRetry())
	}
	channelID := s.lockedChannelID
	if bound != nil {
		if channelID != 0 && channelID != bound.ChannelID {
			return newResponsesWSInvalidRequestError(model.ErrGatewayStateUnavailable)
		}
		channelID = bound.ChannelID
		c.Set(service.GatewayStateContextKey, bound)
	}
	if value, set := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); set {
		explicit, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil || explicit <= 0 {
			return newResponsesWSInvalidRequestError(errors.New("invalid specific channel"))
		}
		if channelID != 0 && channelID != explicit {
			return newResponsesWSInvalidRequestError(model.ErrGatewayStateUnavailable)
		}
		channelID = explicit
	}
	if _, apiErr = middleware.SelectRequestChannel(c, create.Request.Model, channelID); apiErr != nil {
		return apiErr
	}
	info, err = relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIResponses, request, nil)
	if err != nil {
		return types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
	}
	info.IsStream, info.DeferBillingToSend = true, true
	info.ConversionPolicy = types.ConversionLossPolicy(c.GetString("conversion_policy"))
	if info.ConversionPolicy == "" {
		info.ConversionPolicy = types.ConversionLossPolicySafe
	}
	meta := request.GetTokenCountMeta()
	if setting.ShouldCheckPromptSensitive() {
		if contains, _ := service.CheckSensitiveText(meta.CombineText); contains {
			return types.NewError(errors.New("request contains restricted content"), types.ErrorCodeSensitiveWordsDetected, types.ErrOptionWithSkipRetry())
		}
	}
	markLiveRouteSecurityQualified(c)
	tokens, err := service.EstimateRequestToken(c, meta, info)
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(tokens)
	retry := &service.RetryParam{Ctx: c, Retry: common.GetPointer(0)}
	for {
		attemptStarted := time.Now()
		channel, selectErr := getChannel(c, info, retry)
		if selectErr != nil {
			return selectErr
		}
		if !service.ChannelSupportsResponsesWebSocket(channel, create.Request.Model) {
			markLiveRouteCandidateFiltered(c, channel.Id, service.RouteFilterPathUnsupported)
			retry.IncreaseRetry()
			continue
		}
		if s.lockedChannelID != 0 {
			keyAllowed := false
			for _, index := range channel.GetEnabledKeyIndexes() {
				keys := channel.GetKeys()
				if index >= 0 && index < len(keys) && keys[index] == s.lockedKey {
					if _, attempted := service.GetLiveRouteAttemptedKeyIndexes(c, channel.Id)[index]; attempted {
						return stopLiveRouteRetry(c, errors.New("bound websocket credential exhausted its shared attempt budget"))
					}
					common.SetContextKey(c, constant.ContextKeyChannelKey, s.lockedKey)
					common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
					keyAllowed = true
					break
				}
			}
			if !keyAllowed {
				state.closeAfter = true
				return newResponsesWSInvalidRequestError(errors.New("connection credential changed; reconnect required"))
			}
		}
		if err := info.ResetAttemptState(); err != nil {
			return newResponsesWSInvalidRequestError(err)
		}
		info.InitChannelMeta(c)
		if err := relay.PlanRequestForChannel(c, info, channel); err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		adaptor, body, closer, preparationErr := relay.PrepareResponsesRequest(c, info, &create.Request)
		if preparationErr != nil {
			return preparationErr
		}
		data, readErr := io.ReadAll(body)
		closeErr := closer.Close()
		if readErr != nil || closeErr != nil {
			return types.NewError(errors.Join(readErr, closeErr), types.ErrorCodeReadRequestBodyFailed)
		}
		payload, err := buildResponsesWSCreateEvent(data, create.Generate, create.StreamID)
		if err != nil {
			return newResponsesWSInvalidRequestError(err)
		}
		if err := service.PrepareGatewayStateOutput(c, info, channel); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError)
		}
		wireRequest, err := relaychannel.BuildApiRequest(adaptor, c, info, nil)
		if err != nil {
			return types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		identity, err := responsesWSIdentity(channel, info, wireRequest, payload)
		if err != nil {
			return newResponsesWSInvalidRequestError(err)
		}
		if s.lockedIdentity != "" && identity != s.lockedIdentity {
			state.closeAfter = true
			return newResponsesWSInvalidRequestError(errors.New("resolved model or connection settings changed; reconnect required"))
		}
		if err := service.BindGatewayWebSocketIdentity(c, identity); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if err := service.ValidateGatewayOutboundState(c, info, payload); err != nil {
			return newResponsesWSInvalidRequestError(err)
		}
		if err := prepareRelayOutboundPrice(c, info, bytes.NewReader(payload)); err != nil {
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
		}
		markLiveRoutePriceQualified(c)
		if !liveRoutePriceRatioAllowed(c, info.PriceData.ChannelMultiplier()) {
			return newResponsesWSInvalidRequestError(service.ErrRoutePriceRatioExceeded)
		}
		if err := acquireRouteAttemptPermit(c, info, channel, retry.GetRetry()); err != nil {
			if s.lockedChannelID == 0 && markLiveRouteAdmissionRejection(c, channel.Id, err) {
				retry.IncreaseRetry()
				continue
			}
			return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, service.GatewayAdmissionHTTPStatus(err), types.ErrOptionWithSkipRetry())
		}
		if s.getTarget() == nil {
			target, dialErr := dialResponsesWebSocket(c, info, wireRequest)
			if dialErr != nil {
				releaseRouteAttempt(c)
				markLiveRouteCandidateFiltered(c, channel.Id, "websocket_handshake_failed")
				if next, found := nextLiveRouteCandidateIndex(c, retry.GetRetry()); found {
					retry.SetRetry(next)
					continue
				}
				return types.NewError(errors.New("upstream websocket handshake failed"), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if !s.setTarget(target) {
				_ = target.Close()
				return types.NewError(errors.New("websocket closed"), types.ErrorCodeDoRequestFailed)
			}
			s.lockedChannelID, s.lockedKey, s.lockedIdentity = channel.Id, info.ApiKey, identity
			s.lockedUserID, s.lockedTokenID = userID, tokenID
			s.startTargetReader(target)
		}
		// Only response.create crosses the billable boundary. A failed handshake
		// above sent no generation and cannot create an unknown debit.
		if err := info.PrepareUpstreamSend(bytes.NewReader(payload)); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
			state.closeAfter = true
			return types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
		}
		result := s.readCall(c, state, create, info)
		if result == nil || !info.UpstreamRejected || info.HasValidOutput() || state.closeAfter {
			return result
		}
		outcome := relayAttemptOutcome(c, info, result, attemptStarted, false)
		classification := service.ClassifyAttemptOutcome(outcome)
		if !service.CanRouteFailover(classification, outcome.ResponseCommitted, info.HasValidOutput()) {
			return result
		}
		selectionValue, _ := c.Get("route_live_selection")
		selection, _ := selectionValue.(service.LiveRouteSelection)
		// This socket cannot rotate its credential. Check the shared same-key
		// budget before selecting another candidate or replacing the rejection.
		if !selection.Retry.Budget().Allows(classification, service.RouteRetrySameKey, liveRouteRetryCounters(c)) {
			return result
		}
		next, found := nextLiveRouteAttemptForError(c, retry.GetRetry(), channel.Id, classification)
		candidate, exists := selection.CandidateForAttempt(next)
		// Native state and the connection stay bound to one channel. A retry
		// cannot move previous_response_id to a different provider connection.
		if !found || !exists || candidate.ChannelID != s.lockedChannelID {
			return result
		}
		observeLiveRouteAttempt(c, info, outcome)
		if !waitForLiveRouteBackoff(c, info.RetryIndex, result.RetryAfter) {
			return result
		}
		state.terminal = nil
		retry.SetRetry(next)
	}
}

func responsesWSIdentity(channel *model.Channel, info *relaycommon.RelayInfo, request *http.Request, payload []byte) (string, error) {
	var wire struct {
		Model string `json:"model"`
	}
	if err := common.Unmarshal(payload, &wire); err != nil || wire.Model == "" {
		return "", errors.New("final websocket model is missing")
	}
	stableHeaders := request.Header.Clone()
	for _, key := range []string{"X-Request-Id", "X-Client-Request-Id", "Traceparent", "Tracestate", "Baggage", "Content-Length", "Content-Type", "Accept", "User-Agent"} {
		stableHeaders.Del(key)
	}
	data, err := common.Marshal(struct {
		Channel  int
		Type     int
		Endpoint string
		URL      string
		Host     string
		Header   http.Header
		Model    string
		Key      string
		Proxy    string
		Route    interface{}
	}{
		channel.Id, channel.Type, channel.GatewayEndpointID(), request.URL.String(), request.Host, stableHeaders, wire.Model, info.ApiKey, channel.GetSetting().Proxy, channel.GetOtherSettings().AdvancedCustom,
	})
	if err != nil {
		return "", err
	}
	return model.GatewayDigest(string(data)), nil
}

func dialResponsesWebSocket(c *gin.Context, info *relaycommon.RelayInfo, request *http.Request) (*websocket.Conn, error) {
	// Gorilla performs an HTTP/1 upgrade. HTTP/2 shard transports do not apply
	// to this connection, but its proxy and TLS configuration still do.
	settings := info.ChannelSetting
	settings.HTTPProtocol = dto.HTTPProtocolHTTP1
	settings.HTTP2ConnectionShards = 1
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, err
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return nil, errors.New("unsupported websocket transport")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second, Proxy: transport.Proxy, NetDialContext: transport.DialContext, TLSClientConfig: transport.TLSClientConfig}
	endpoint := *request.URL
	switch endpoint.Scheme {
	case "https":
		endpoint.Scheme = "wss"
	case "http":
		endpoint.Scheme = "ws"
	case "ws", "wss":
	default:
		return nil, errors.New("unsupported websocket scheme")
	}
	headers := request.Header.Clone()
	for _, key := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Content-Length", "Accept"} {
		headers.Del(key)
	}
	if request.Host != "" {
		headers.Set("Host", request.Host)
	}
	connection, response, err := dialer.DialContext(c.Request.Context(), endpoint.String(), headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	limit := int64(constant.MaxRequestBodyMB)
	if limit <= 0 {
		limit = 128
	}
	connection.SetReadLimit(limit << 20)
	return connection, nil
}

func (s *responsesWSSession) readCall(c *gin.Context, state *responsesWSCallState, create responsesWSCreateRequest, info *relaycommon.RelayInfo) *types.NewAPIError {
	accumulator := service.NewResponsesUsageAccumulator(info)
	info.StreamStatus = relaycommon.NewStreamStatus()
	timeout := time.Duration(constant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	idle := time.NewTimer(timeout)
	defer idle.Stop()
	defer func() {
		// Settlement uses these counters as an estimate when the terminal
		// event or its usage never arrived.
		if !info.UpstreamRejected {
			info.CaptureResponseUsage(accumulator.Finish(), "text", nil)
		}
	}()
	var responseID string
	var control []byte
	controlSent := false
	for {
		select {
		case incoming := <-state.inbox:
			if incoming.err != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, incoming.err)
				state.closeAfter = true
				return types.NewError(incoming.err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			idle.Reset(timeout)
			var event struct {
				dto.ResponsesStreamResponse
				StreamID string `json:"stream_id"`
			}
			if err := common.Unmarshal(incoming.body, &event); err != nil {
				state.closeAfter = true
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
				return types.NewError(errors.New("invalid upstream websocket event"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			if event.StreamID != "" && event.StreamID != create.StreamID {
				continue
			}
			if event.Response != nil && event.Response.ID != "" && event.Response.ID == s.lastResponseID {
				continue
			}
			info.SetFirstResponseTime()
			if event.Type == "error" {
				var rejection responsesWSErrorEvent
				if err := common.Unmarshal(incoming.body, &rejection); err != nil {
					return newResponsesWSInvalidRequestError(err)
				}
				terminal, ambiguous, controlError := responsesWSErrorEndsRequest(rejection, create.StreamID, responseID, control)
				if !terminal {
					if controlError {
						control = nil
						controlSent = false
					}
					if err := s.writeClient(incoming.kind, incoming.body); err != nil {
						state.closeAfter = true
						return types.NewError(err, types.ErrorCodeBadResponse)
					}
					continue
				}
				info.UpstreamRejected = !info.UpstreamAccepted && !ambiguous && rejection.Status >= 400 && rejection.Status < 500 && rejection.Status != 408
				state.terminal, state.closeAfter = &incoming, ambiguous
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("upstream websocket request rejected"))
				if info.UpstreamRejected {
					info.ResponseUsage = nil
				}
				if info.UpstreamRejected && rejection.Error != nil {
					return types.WithOpenAIError(*rejection.Error, rejection.Status)
				}
				return types.NewErrorWithStatusCode(errors.New("upstream websocket request rejected"), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
			if strings.HasPrefix(event.Type, "response.") {
				info.UpstreamAccepted = true
			}
			if event.Response != nil && event.Response.ID != "" {
				responseID = event.Response.ID
			}
			if info.UpstreamAccepted && control != nil && !controlSent {
				if err := s.writeTarget(websocket.TextMessage, control); err != nil {
					state.closeAfter = true
					return types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
				}
				controlSent = true
			}
			accumulator.Observe(&event.ResponsesStreamResponse, incoming.body)
			termination := relayconvert.TerminationFromResponse(&event.ResponsesStreamResponse)
			if termination.IsTerminal() {
				c.Set("response_termination", termination)
			}
			if err := service.BindGatewayOutput(c, incoming.body); err != nil {
				state.closeAfter = true
				return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			if semantic, _ := helper.StreamPayloadState(string(incoming.body)); semantic && (!termination.IsTerminal() || termination.IsSuccessful()) {
				info.MarkValidOutput()
			}
			if termination.IsTerminal() {
				s.lastResponseID = responseID
				state.terminal = &incoming
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				if !termination.IsSuccessful() {
					info.StreamStatus.RecordError("upstream generation did not complete successfully")
				}
				return nil
			}
			if err := s.writeClient(incoming.kind, incoming.body); err != nil {
				state.closeAfter = true
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
				return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
		case pending := <-state.controls:
			if pending.streamID != "" && pending.streamID != create.StreamID {
				s.sendError(pending.eventID, pending.streamID, newResponsesWSInvalidRequestError(errors.New("cancellation stream_id does not match the active response")))
				continue
			}
			if control != nil {
				s.sendError(pending.eventID, pending.streamID, newResponsesWSInvalidRequestError(errors.New("a cancellation is already pending")))
				continue
			}
			control = pending.body
			if info.UpstreamAccepted {
				if err := s.writeTarget(websocket.TextMessage, control); err != nil {
					state.closeAfter = true
					return types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
				}
				controlSent = true
			}
		case <-idle.C:
			state.closeAfter = true
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, errors.New("upstream websocket timed out"))
			return types.NewError(errors.New("upstream websocket timed out"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		case <-c.Request.Context().Done():
			state.closeAfter = true
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
			return types.NewError(c.Request.Context().Err(), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
	}
}

func (s *responsesWSSession) startTargetReader(target *websocket.Conn) {
	s.workers.Go(func() {
		for {
			kind, body, err := target.ReadMessage()
			state := s.getCurrent()
			if state == nil {
				if err != nil {
					s.shutdown()
					return
				}
				continue
			}
			select {
			case state.inbox <- responsesWSMessage{kind: kind, body: body, err: err}:
			case <-state.done:
			case <-s.ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	})
}
