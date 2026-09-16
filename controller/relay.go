package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)

	var (
		newAPIError *types.NewAPIError
		relayInfo   *relaycommon.RelayInfo
		ws          *websocket.Conn
	)
	defer func() {
		releaseRouteAttempt(c)
		finalizeLiveRouteDecision(c, relayInfo, newAPIError)
		if newAPIError != nil && !relayResponseCommitted(c, relayInfo) {
			middleware.MarkRelayRequestFailed(c)
			return
		}
		if relayInfo == nil || relayInfo.StreamStatus == nil {
			return
		}
		if !relayInfo.StreamStatus.IsNormalEnd() || relayInfo.StreamStatus.HasErrors() {
			middleware.MarkRelayRequestFailed(c)
		}
	}()

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil && relayInfo != nil && relayInfo.RelayMode == relayconstant.RelayModeAudioSpeech &&
			!relayInfo.IsStream && relayResponseCommitted(c, relayInfo) && c.Request.Context().Err() == nil {
			// Binary audio has no error event. Abort the HTTP body so a truncated
			// provider response cannot look like a successfully completed file.
			panic(http.ErrAbortHandler)
		}
		if newAPIError != nil && relayInfo != nil && relayInfo.IsStream && relayResponseCommitted(c, relayInfo) && !c.GetBool("gateway_stream_error_sent") && c.Request.Context().Err() == nil {
			writeRelayStreamFailure(c, relayFormat, newAPIError)
		}
		if newAPIError != nil && !relayResponseCommitted(c, relayInfo) && !requestContextDone(c) {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	relayInfo.ConversionPolicy = types.ConversionLossPolicy(c.GetString("conversion_policy"))
	if relayInfo.ConversionPolicy == "" {
		relayInfo.ConversionPolicy = types.ConversionLossPolicySafe
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}
	if routeLiveSelectionActive(c) {
		markLiveRouteSecurityQualified(c)
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)
	relayInfo.DeferBillingToSend = true

	defer func() {
		if relayInfo.ResponseUsage != nil && (newAPIError == nil || relayInfo.HasValidOutput()) {
			if relayInfo.ResponseUsageKind == "realtime" {
				usage := relayInfo.ResponseUsage
				service.PostWssConsumeQuota(c, relayInfo, relayInfo.BillingModelName(), &dto.RealtimeUsage{
					InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens,
					InputTokenDetails: usage.PromptTokensDetails, OutputTokenDetails: usage.CompletionTokenDetails,
				}, strings.Join(relayInfo.ResponseUsageLogContent, ", "))
			} else if relayInfo.ResponseUsageKind == "audio" {
				service.PostAudioConsumeQuota(c, relayInfo, relayInfo.ResponseUsage, strings.Join(relayInfo.ResponseUsageLogContent, ", "))
			} else {
				service.PostTextConsumeQuota(c, relayInfo, relayInfo.ResponseUsage, relayInfo.ResponseUsageLogContent)
			}
			if relayInfo.BillingSettlementError != nil {
				logger.LogError(c, "billing settlement remains pending: "+relayInfo.BillingSettlementError.Error())
				if !relayResponseCommitted(c, relayInfo) {
					newAPIError = types.NewError(relayInfo.BillingSettlementError, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
				}
			}
			return
		}
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:   c,
		Retry: common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil
	compactRetry := newCompactRetryState(relayInfo)

	for {
		if _, exists := c.Get("route_channel_permit"); exists {
			releaseRouteAttempt(c)
			if routeLiveSelectionActive(c) && c.GetBool(liveRouteRenewalFailedKey) {
				break
			}
		}
		if routeLiveSelectionActive(c) && liveRouteRetryCounters(c).TotalAttempts >= service.DefaultTotalAttempts {
			break
		}
		if compactRetry == nil {
			if !routeLiveSelectionActive(c) && retryParam.GetRetry() > relayRetryLimit(c) {
				break
			}
		} else {
			compactRetry.prepare(retryParam, relayInfo)
		}
		if compactRetry == nil {
			if routeLiveSelectionActive(c) {
				relayInfo.RetryIndex = liveRouteRetryCounters(c).TotalAttempts
			} else {
				relayInfo.RetryIndex = retryParam.GetRetry()
			}
		} else {
			relayInfo.RetryIndex = len(c.GetStringSlice("use_channel"))
		}
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			if relayInfo.LastError != nil && errors.Is(channelErr, service.ErrRouteSelectionUnavailable) {
				newAPIError = relayInfo.LastError
			}
			if compactRetry != nil && compactRetry.switchToBase(c, relayInfo) {
				continue
			}
			break
		}
		if err := relayInfo.ResetAttemptState(); err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			break
		}
		helper.ResetStreamOutputError(c)
		relayInfo.InitChannelMeta(c)
		relayInfo.SetEstimatePromptTokens(tokens)
		c.Set("gateway_last_attempt_outcome", nil)
		c.Set("gateway_health_epoch", int64(0))
		c.Set("gateway_token_budget", int64(0))
		c.Set("gateway_token_budget_source", "")
		if compactRetry != nil {
			if err := helper.ConfigureCompactAttempt(c, relayInfo); err != nil {
				newAPIError = types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
				break
			}
			common.SetContextKey(c, constant.ContextKeyOriginalModel, relayInfo.BillingModelName())
		}
		if planErr := relay.PlanRequestForChannel(c, relayInfo, channel); planErr != nil {
			markLiveRouteCandidateFiltered(c, channel.Id, "conversion_unavailable")
			if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			newAPIError = types.NewErrorWithStatusCode(planErr, types.ErrorCodeConvertRequestFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			break
		}
		if _, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta); err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
			break
		}
		markLiveRoutePriceQualified(c)
		if routeLiveSelectionActive(c) && !liveRoutePriceRatioAllowed(c, relayInfo.PriceData.ChannelRatio) {
			markLiveRouteCandidateFiltered(c, channel.Id, service.RouteFilterPriceForbidden)
			if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			newAPIError = types.NewErrorWithStatusCode(
				service.ErrRoutePriceRatioExceeded,
				types.ErrorCodeModelPriceError,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
			)
			break
		}
		if permitErr := acquireRouteAttemptPermit(c, relayInfo, channel, retryParam.GetRetry()); permitErr != nil {
			if errors.Is(permitErr, service.ErrAdmissionCapacity) {
				markLiveRouteCandidateFiltered(c, channel.Id, "capacity_exhausted")
			}
			if service.LiveRouteQualificationAllowsFailover(permitErr) && !relayResponseCommitted(c, relayInfo) {
				if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
				if nextAttempt, found := queueBusyLiveRouteCandidate(c); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
			}
			newAPIError = types.NewErrorWithStatusCode(permitErr, types.ErrorCodeGetChannelFailed, service.GatewayAdmissionHTTPStatus(permitErr), types.ErrOptionWithSkipRetry())
			break
		}
		if stateErr := service.PrepareGatewayStateOutput(c, relayInfo, channel); stateErr != nil {
			newAPIError = types.NewErrorWithStatusCode(stateErr, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry(), types.ErrOptionWithHideErrMsg(service.GatewayStatePublicMessage(stateErr)))
			break
		}
		if relayInfo.BeforeSend == nil {
			relayInfo.BeforeSend = func(body io.Reader) error {
				if err := prepareRelayOutboundPrice(c, relayInfo, body); err != nil {
					return err
				}
				if err := service.PrepareBillingForSelectedModel(c, relayInfo); err != nil {
					return err
				}
				return nil
			}
		}
		addUsedChannel(c, channel.Id)
		if compactRetry != nil {
			compactRetry.recordAttempt(c, channel.Id)
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)
		attemptStartedAt := time.Now()

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}
		if outputErr := helper.StreamOutputError(c); outputErr != nil {
			if errors.Is(outputErr, helper.ErrStreamClientWrite) {
				if relayInfo.StreamStatus == nil {
					relayInfo.StreamStatus = relaycommon.NewStreamStatus()
				}
				relayInfo.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, outputErr)
			}
			if newAPIError == nil {
				newAPIError = types.NewErrorWithStatusCode(outputErr, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
		}
		if stateFailure, exists := c.Get(service.GatewayStateOutputErrorKey); exists {
			if stateErr, ok := stateFailure.(error); ok {
				newAPIError = types.NewErrorWithStatusCode(stateErr, types.ErrorCodeUpdateDataError, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry(), types.ErrOptionWithHideErrMsg(service.GatewayStatePublicMessage(stateErr)))
			}
		}
		attemptOutcome := relayAttemptOutcome(c, relayInfo, newAPIError, attemptStartedAt, false)
		observeLiveRouteAttempt(c, relayInfo, attemptOutcome)
		if markLiveRoutePreSendRejection(c, relayInfo, newAPIError) {
			if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			if nextAttempt, found := queueBusyLiveRouteCandidate(c); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			break
		}

		if newAPIError == nil {
			if routeLiveSelectionActive(c) && shouldFailLiveRouteAfterRenewalFailure(c, relayInfo) {
				// A successful upstream response is not enough to commit a live
				// attempt after its health or Provider reservation was lost. The
				// admission error is handled below without penalizing provider
				// health or splicing a second response after output was committed.
				newAPIError = types.NewErrorWithStatusCode(service.ErrGatewayStorageUnavailable, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
			}
		}
		if newAPIError == nil {
			if relayInfo.BillingSettlementError != nil {
				// The upstream response may already be committed. Never emit a
				// second protocol response or retry another provider after that
				// boundary; retain the error on RelayInfo for recovery/observability.
				logger.LogError(c, "billing settlement remains pending: "+relayInfo.BillingSettlementError.Error())
				if !relayResponseCommitted(c, relayInfo) {
					newAPIError = types.NewError(relayInfo.BillingSettlementError, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
				}
				break
			}
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		liveNextAttempt := -1
		liveClassification := service.RouteErrorClassification{}
		if routeLiveSelectionActive(c) {
			liveClassification = service.ClassifyAttemptOutcome(attemptOutcome)
			responseCommitted := c.Writer != nil && c.Writer.Written()
			streamCommitted := responseCommitted || relayInfo.HasValidOutput() || relayFormat == types.RelayFormatOpenAIRealtime
			if service.CanRouteFailover(liveClassification, streamCommitted, relayInfo.HasValidOutput()) {
				if nextAttempt, found := nextLiveRouteAttemptForError(c, retryParam.GetRetry(), channel.Id, liveClassification); found {
					liveNextAttempt = nextAttempt
				}
			}
		}

		modelSemanticError := compactRetry != nil && compactRetry.stage == relaycommon.CompactAttemptExact && isCompactModelSemanticError(newAPIError)
		channelError := types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
		if modelSemanticError {
			channelError.AutoBan = false
		}
		if routeLiveSelectionActive(c) {
			// Live routing isolates failures at key, capability, or channel/model
			// scope. Reusing legacy AutoBan here would widen a scoped failure into
			// a global Channel.Status change and bypass the new health state.
			channelError.AutoBan = false
		}
		processChannelError(c, *channelError, newAPIError)
		if compactRetry == nil && routeLiveSelectionActive(c) && liveNextAttempt < 0 {
			break
		}

		if compactRetry != nil {
			if compactRetry.advance(c, relayInfo, newAPIError, !relayResponseCommitted(c, relayInfo) && (liveClassification.Retryable || liveClassification.Failoverable || modelSemanticError)) {
				continue
			}
			break
		}

		if routeLiveSelectionActive(c) {
			retryAfter := liveRouteScopedRetryAfter(c, liveNextAttempt, channel.Id, newAPIError.RetryAfter)
			if !waitForLiveRouteBackoff(c, relayInfo.RetryIndex, retryAfter) {
				break
			}
			retryParam.SetRetry(liveNextAttempt)
			continue
		}
		break
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil && !relayInfo.HasSendResponse() {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

// relayResponseCommitted is the failover boundary for a live attempt. The
// first SSE frame can be observed before its payload is parsed, so
// FirstResponseTime alone is not evidence that a valid response was sent.
func relayResponseCommitted(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c != nil && c.Writer != nil && c.Writer.Written() {
		return true
	}
	return info != nil && info.HasValidOutput()
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

type compactRetryState struct {
	stage         relaycommon.CompactAttemptStage
	exactBudget   int
	baseBudget    int
	exactAttempts int
	baseAttempts  int
	attemptedKeys map[relaycommon.CompactAttemptStage]service.CompactAttemptedKeyIndexes
}

func newCompactRetryState(info *relaycommon.RelayInfo) *compactRetryState {
	if info == nil || info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return nil
	}
	totalAttempts := service.DefaultTotalAttempts
	if totalAttempts < 1 {
		totalAttempts = 1
	}
	stage := info.CompactAttemptStage
	if stage == relaycommon.CompactAttemptNone {
		stage = relaycommon.CompactAttemptBase
		if ratio_setting.IsGPTCompactBaseModel(info.RequestedModel) {
			stage = relaycommon.CompactAttemptExact
		}
	} else if !ratio_setting.IsGPTCompactBaseModel(info.RequestedModel) {
		stage = relaycommon.CompactAttemptBase
	}
	state := &compactRetryState{
		stage:       stage,
		exactBudget: (2*totalAttempts + 2) / 3,
		baseBudget:  totalAttempts - (2*totalAttempts+2)/3,
		attemptedKeys: map[relaycommon.CompactAttemptStage]service.CompactAttemptedKeyIndexes{
			relaycommon.CompactAttemptExact: {},
			relaycommon.CompactAttemptBase:  {},
		},
	}
	if stage == relaycommon.CompactAttemptBase {
		state.baseBudget += state.exactBudget
	}
	return state
}

func (s *compactRetryState) prepare(param *service.RetryParam, info *relaycommon.RelayInfo) {
	info.CompactAttemptStage = s.stage
	info.UpstreamAttemptModel = ""
	service.SetCompactAttemptedKeyIndexes(param.Ctx, s.attemptedKeys[s.stage])
	if s.stage == relaycommon.CompactAttemptExact {
		param.SetRetry(s.exactAttempts)
		maxRetries := s.exactBudget - 1
		param.MaxRetries = &maxRetries
	} else {
		param.SetRetry(s.baseAttempts)
		maxRetries := s.baseBudget - 1
		param.MaxRetries = &maxRetries
	}
}

func (s *compactRetryState) recordAttempt(c *gin.Context, channelID int) {
	keyIndex := 0
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		keyIndex = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	channels := s.attemptedKeys[s.stage]
	if channels[channelID] == nil {
		channels[channelID] = make(map[int]struct{})
	}
	channels[channelID][keyIndex] = struct{}{}
	if s.stage == relaycommon.CompactAttemptExact {
		s.exactAttempts++
		return
	}
	s.baseAttempts++
}

func (s *compactRetryState) remainingAttempts() int {
	if s.stage == relaycommon.CompactAttemptExact {
		return s.exactBudget - s.exactAttempts
	}
	return s.baseBudget - s.baseAttempts
}

func (s *compactRetryState) switchToBase(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if s.stage != relaycommon.CompactAttemptExact || s.baseAttempts >= s.baseBudget {
		return false
	}
	if unusedExact := s.exactBudget - s.exactAttempts; unusedExact > 0 {
		s.baseBudget += unusedExact
	}
	s.stage = relaycommon.CompactAttemptBase
	info.CompactAttemptStage = s.stage
	info.UpstreamAttemptModel = ""
	service.SetCompactStage(c, s.stage)
	service.SetCompactAttemptedKeyIndexes(c, s.attemptedKeys[s.stage])
	return true
}

func (s *compactRetryState) advance(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, retryable bool) bool {
	if c.GetBool("gateway_retry_stopped") || relayResponseCommitted(c, info) || requestContextDone(c) {
		return false
	}
	if s.stage == relaycommon.CompactAttemptExact && isCompactModelSemanticError(apiErr) {
		return s.switchToBase(c, info)
	}
	if !retryable {
		return false
	}
	if s.remainingAttempts() > 0 {
		return true
	}
	return s.switchToBase(c, info)
}

func isCompactModelSemanticError(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	openAIError := apiErr.ToOpenAIError()
	code := strings.ToLower(strings.TrimSpace(fmt.Sprint(openAIError.Code)))
	switch code {
	case "model_not_found", "unsupported_model", "model_not_supported", "invalid_model", "unknown_model":
		return true
	}
	if apiErr.StatusCode != http.StatusBadRequest && apiErr.StatusCode != http.StatusNotFound && apiErr.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(openAIError.Param), "model") {
		return true
	}
	message := strings.ToLower(openAIError.Message + " " + apiErr.Error())
	if !strings.Contains(message, "model") {
		return false
	}
	for _, marker := range []string{
		"unknown model",
		"unsupported model",
		"invalid model",
		"no such model",
		"not found",
		"does not exist",
		"not available",
		"not supported",
		"unavailable",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		specificChannelID := 0
		if value, ok := c.Get("route_live_selection"); ok {
			selection, valid := value.(service.LiveRouteSelection)
			if valid {
				specificChannelID = selection.SpecificChannelID
			}
		}
		channel, err := middleware.SelectRequestChannel(c, ratio_setting.WithCompactModelSuffix(info.RequestedModel), specificChannelID, info.CompactAttemptStage)
		retryParam.SetRetry(0)
		if err == nil && info.LastError != nil && liveRouteRetryCounters(c).TotalAttempts > 0 {
			value, _ := c.Get("route_live_selection")
			selection := value.(service.LiveRouteSelection)
			previous, _ := c.Get("gateway_last_attempt_outcome")
			outcome, _ := previous.(service.AttemptOutcome)
			classification := service.ClassifyAttemptOutcome(outcome)
			if info.CompactAttemptStage == relaycommon.CompactAttemptBase && isCompactModelSemanticError(info.LastError) {
				classification.Retryable = true
			}
			candidate, index, found := selection.NextCandidateForError(-1, c.GetInt(liveRoutePreviousChannelKey), classification, liveRouteRetryCounters(c))
			if !found {
				return nil, types.NewError(service.ErrRouteSelectionUnavailable, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
			}
			if !waitForLiveRouteBackoff(c, info.RetryIndex, liveRouteScopedRetryAfter(c, index, c.GetInt(liveRoutePreviousChannelKey), outcome.RetryAfter)) {
				return nil, stopLiveRouteRetry(c, errors.New("route retry deadline exceeded"))
			}
			retryParam.SetRetry(index)
			if candidate.ChannelID != channel.Id {
				var loadErr error
				channel, loadErr = model.GetChannelById(candidate.ChannelID, true)
				if loadErr != nil {
					return nil, types.NewError(loadErr, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
				}
				err = middleware.SetupContextForSelectedChannel(c, channel, ratio_setting.WithCompactModelSuffix(info.RequestedModel))
			}
		}
		if err == nil {
			err = scanLiveRouteCandidate(c, channel.Id)
		}
		return channel, err
	}
	value, ok := c.Get("route_live_selection")
	if !ok {
		return nil, types.NewError(service.ErrRouteSelectionUnavailable, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	selection, valid := value.(service.LiveRouteSelection)
	if !valid || selection.Source == service.RouteSourceUnavailable {
		return nil, types.NewError(service.ErrRouteSelectionUnavailable, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	for candidateIndex := retryParam.GetRetry(); candidateIndex < len(selection.Attempts); candidateIndex++ {
		candidate, actualIndex, found := selection.CandidateAtOrAfter(candidateIndex)
		if !found {
			break
		}
		if scanErr := scanLiveRouteCandidate(c, candidate.ChannelID); scanErr != nil {
			return nil, scanErr
		}
		channel, err := model.GetChannelById(candidate.ChannelID, true)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if channel.Status != common.ChannelStatusEnabled {
			markLiveRouteCandidateFiltered(c, candidate.ChannelID, service.RouteFilterChannelDisabled)
			candidateIndex = actualIndex
			continue
		}
		retryParam.SetRetry(actualIndex)
		if actualIndex == 0 && common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id {
			return channel, nil
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, channel, info.BillingModelName()); setupErr != nil {
			if setupErr.GetErrorCode() == types.ErrorCodeChannelNoAvailableKey {
				markLiveRouteCandidateFiltered(c, candidate.ChannelID, service.RouteFilterKeyUnavailable)
				candidateIndex = actualIndex
				continue
			}
			return nil, setupErr
		}
		return channel, nil
	}
	return nil, types.NewError(service.ErrRouteSelectionUnavailable, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
}

func acquireRouteAttemptPermit(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, attempt int) error {
	if c == nil || info == nil || channel == nil {
		return service.ErrRouteSelectionUnavailable
	}
	value, ok := c.Get("route_live_selection")
	if !ok {
		return service.ErrRouteSelectionUnavailable
	}
	selection, ok := value.(service.LiveRouteSelection)
	if !ok || selection.Source == service.RouteSourceUnavailable {
		return service.ErrRouteSelectionUnavailable
	}
	candidate, ok := selection.CandidateForAttempt(attempt)
	if !ok || candidate.ChannelID != channel.Id {
		return service.ErrRouteSelectionUnavailable
	}
	info.RouteCapabilityModel = candidate.RequestModel
	expected, err := service.GetRouteRuntimeState(c.Request.Context(), channel.Id, info.RouteCapabilityModel)
	if err != nil {
		return err
	}
	if candidate.SnapshotVersion <= 0 || expected.CapabilityVersion != candidate.SnapshotVersion {
		return service.ErrAdmissionRuntime
	}
	if candidate.HealthEpoch <= 0 || expected.HealthEpoch != candidate.HealthEpoch {
		return service.ErrAdmissionRuntime
	}
	if expected.ChannelRatio != info.PriceData.ChannelRatio {
		return service.ErrAdmissionRuntime
	}
	qualification, qualified := liveRouteRuntimeQualificationForRequest(c)
	if !qualified {
		return &service.LiveRouteQualificationError{Reason: "runtime_qualification_unavailable"}
	}
	permit, admittedChannel, err := service.AcquireGatewayChannelPermit(c.Request.Context(), info.RequestId, channel.Id, false, service.NewGatewayWaitBudget(0))
	if errors.Is(err, service.ErrGatewayWaitExpired) || errors.Is(err, service.ErrGatewayQueueFull) {
		err = service.ErrAdmissionCapacity
	}
	if errors.Is(err, service.ErrAdmissionCapacity) && c.GetBool("route_queue_selected") && c.GetInt("route_queue_attempt") == attempt {
		if _, budget, ok := service.GatewayAdmissionFromContext(c.Request.Context()); ok {
			c.Set("route_queue_attempt", -1)
			permit, admittedChannel, err = service.AcquireGatewayChannelPermit(c.Request.Context(), info.RequestId, channel.Id, true, budget)
		}
	}
	if err != nil {
		return err
	}
	if admittedChannel.ChannelRatio != expected.ChannelRatio {
		return releaseRejectedRouteAttempt(c, permit, service.ErrAdmissionRuntime)
	}
	boundKey, _, stateErr := service.ValidateGatewayStateOwner(c, admittedChannel.Id)
	if stateErr != nil {
		return releaseRejectedRouteAttempt(c, permit, stateErr)
	}
	if boundKey != "" && boundKey != common.GetContextKeyString(c, constant.ContextKeyChannelKey) {
		return releaseRejectedRouteAttempt(c, permit, model.ErrGatewayStateUnavailable)
	}
	current, err := service.GetRouteRuntimeState(c.Request.Context(), channel.Id, info.RouteCapabilityModel)
	if err != nil {
		return releaseRejectedRouteAttempt(c, permit, err)
	}
	if err := service.RecheckRouteRuntime(expected, current); err != nil {
		return releaseRejectedRouteAttempt(c, permit, err)
	}
	if err := service.RecheckLiveRouteCandidate(service.LiveRouteCandidateQualificationRequest{
		Context:                  c.Request.Context(),
		RouteSource:              selection.Source,
		UserID:                   info.UserId,
		TokenID:                  info.TokenId,
		ChannelID:                channel.Id,
		RequestModel:             candidate.RequestModel,
		PermissionModel:          info.BillingModelName(),
		IsPlayground:             info.IsPlayground,
		SpecificChannelID:        selection.SpecificChannelID,
		RequestPath:              c.Request.URL.Path,
		ExpectedSnapshotVersion:  candidate.SnapshotVersion,
		ExpectedCatalogVersion:   candidate.CatalogVersion,
		ExpectedProfileVersion:   selection.Decision.ConfigurationVersion,
		PriceEligibilityKnown:    qualification.PriceEligibilityKnown,
		PriceEligible:            qualification.PriceEligible,
		SecurityEligibilityKnown: qualification.SecurityEligibilityKnown,
		SecurityAllowed:          qualification.SecurityAllowed,
	}); err != nil {
		if reason := service.LiveRouteQualificationReason(err); reason != "" {
			markLiveRouteCandidateFiltered(c, channel.Id, reason)
		}
		return releaseRejectedRouteAttempt(c, permit, err)
	}
	current, err = service.GetRouteRuntimeState(c.Request.Context(), channel.Id, info.RouteCapabilityModel)
	if err != nil {
		return releaseRejectedRouteAttempt(c, permit, err)
	}
	if err := service.RecheckRouteRuntime(expected, current); err != nil {
		return releaseRejectedRouteAttempt(c, permit, err)
	}
	c.Set("route_channel_permit", permit)
	markLiveRouteAttempt(c, channel.Id, service.AdmissionStateAcquired)
	// Provider reservations and health probes require maintenance while the
	// actual upstream work is active; local permits never expire or renew.
	parentContext := c.Request.Context()
	if err := parentContext.Err(); err != nil {
		return releaseRejectedRouteAttempt(c, permit, err)
	}
	attemptContext, cancel := context.WithCancel(parentContext)
	c.Request = c.Request.WithContext(attemptContext)
	healthState := &routeAttemptHealthState{}
	c.Set("route_live_health", healthState)
	info.UpstreamSent, info.UpstreamAccepted = false, false
	info.BeforeSend = func(body io.Reader) error { return prepareLiveRouteSend(c, info, healthState, uuid.NewString(), body) }
	renewal := service.StartAttemptMaintenance(attemptContext, 30*time.Second, healthState.renew)
	go func() {
		if err, ok := <-renewal.Done; ok && err != nil {
			// Stop upstream work when its health or Provider reservation is lost.
			// The lifecycle owner releases the local permit after work has stopped.
			cancel()
		}
	}()
	c.Set("route_attempt_cancel", cancel)
	c.Set("route_attempt_parent_context", parentContext)
	c.Set("route_live_renewal", renewal)
	return nil
}

func releaseRejectedRouteAttempt(c *gin.Context, permit *service.Permit, cause error) error {
	permit.Release()
	markLiveRouteAttempt(c, permit.ChannelID, service.AdmissionStateReleased)
	return cause
}

func stopRouteAttemptRenewal(c *gin.Context) {
	if c == nil {
		return
	}
	if renewalValue, exists := c.Get("route_live_renewal"); exists {
		if renewal, valid := renewalValue.(service.AttemptMaintenance); valid && renewal.Stop != nil {
			renewal.Stop()
			if renewal.Done != nil {
				<-renewal.Done
			}
			if renewal.Failure != nil && renewal.Failure() != nil {
				c.Set(liveRouteRenewalFailedKey, true)
				channelID := c.GetInt("channel_id")
				if permitValue, exists := c.Get("route_channel_permit"); exists {
					if permit, valid := permitValue.(*service.Permit); valid {
						channelID = permit.ChannelID
					}
				}
				if channelID <= 0 {
					if selectionValue, exists := c.Get("route_live_selection"); exists {
						if selection, valid := selectionValue.(service.LiveRouteSelection); valid {
							channelID = selection.Decision.SelectedChannelID
							if channelID <= 0 && len(selection.Attempts) > 0 {
								channelID = selection.Attempts[0].ChannelID
							}
						}
					}
				}
				markLiveRouteAttempt(c, channelID, service.AdmissionStateRenewalFailed)
			}
		}
		c.Set("route_live_renewal", nil)
	}
}

func releaseRouteAttempt(c *gin.Context) {
	if c == nil {
		return
	}
	stopRouteAttemptRenewal(c)
	if value, exists := c.Get("route_live_health"); exists {
		if state, ok := value.(*routeAttemptHealthState); ok {
			state.mu.Lock()
			if state.attempt != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := service.ReleaseRouteAttemptHealth(ctx, *state.attempt); err != nil {
					recordLiveRouteGovernanceFailure(c, state.attempt.ChannelID, "health_release_failed", err)
				}
				cancel()
				state.attempt = nil
			}
			if state.rate != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := service.FinishGatewayProviderRate(ctx, common.RDB, *state.rate, 0, false, false, 0); err != nil {
					recordLiveRouteGovernanceFailure(c, 0, "provider_rate_release_failed", err)
				}
				cancel()
				state.rate = nil
			}
			state.mu.Unlock()
		}
		c.Set("route_live_health", nil)
	}
	if cancelValue, exists := c.Get("route_attempt_cancel"); exists {
		if cancel, valid := cancelValue.(context.CancelFunc); valid && cancel != nil {
			cancel()
		}
		c.Set("route_attempt_cancel", nil)
	}
	if parentValue, exists := c.Get("route_attempt_parent_context"); exists && c.Request != nil {
		if parentContext, valid := parentValue.(context.Context); valid && parentContext != nil {
			c.Request = c.Request.WithContext(parentContext)
		}
		c.Set("route_attempt_parent_context", nil)
	}
	value, ok := c.Get("route_channel_permit")
	permit, valid := value.(*service.Permit)
	if ok && valid {
		permit.Release()
		markLiveRouteAttempt(c, permit.ChannelID, service.AdmissionStateReleased)
	}
	c.Set("route_channel_permit", nil)
}

func recordLiveRouteGovernanceFailure(c *gin.Context, channelID int, code string, err error) {
	if err != nil {
		common.SysError(fmt.Sprintf("live route governance update failed (channel=%d, code=%s): %v", channelID, code, err))
	}
	if c == nil {
		return
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if !ok || !valid || selection.Source == service.RouteSourceUnavailable {
		return
	}
	selection.Decision.SetFinalError(service.RouteErrorClass(code))
	c.Set("route_live_selection", selection)
}

func liveRouteRenewalFailed(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if c.GetBool(liveRouteRenewalFailedKey) {
		return true
	}
	value, exists := c.Get("route_live_renewal")
	if !exists {
		return false
	}
	renewal, valid := value.(service.AttemptMaintenance)
	if !valid || renewal.Failure == nil || renewal.Failure() == nil {
		return false
	}
	c.Set(liveRouteRenewalFailedKey, true)
	return true
}

// shouldFailLiveRouteAfterRenewalFailure preserves an already committed
// upstream response. The maintenance failure is still recorded by the common
// release path, but it must not turn a delivered response into a refund.
func shouldFailLiveRouteAfterRenewalFailure(c *gin.Context, info *relaycommon.RelayInfo) bool {
	return liveRouteRenewalFailed(c) && !relayResponseCommitted(c, info)
}

func markLiveRouteAttempt(c *gin.Context, channelID int, state string) {
	if c == nil || channelID <= 0 {
		return
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if !ok || !valid || selection.Source == service.RouteSourceUnavailable {
		return
	}
	selection.Decision.SelectedChannelID = channelID
	selection.Decision.AdmissionState = state
	for index := range selection.Decision.Candidates {
		if selection.Decision.Candidates[index].ChannelID == channelID {
			selection.Decision.Candidates[index].AdmissionState = state
		}
	}
	c.Set("route_live_selection", selection)
}

func markLiveRouteCandidateFiltered(c *gin.Context, channelID int, reason string) {
	if c == nil || channelID <= 0 || reason == "" {
		return
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if !ok || !valid || selection.Source == service.RouteSourceUnavailable {
		return
	}
	for index := range selection.Decision.Candidates {
		if selection.Decision.Candidates[index].ChannelID == channelID && selection.Decision.Candidates[index].FilterReason == "" {
			selection.Decision.Candidates[index].FilterReason = reason
			selection.Decision.Candidates[index].AdmissionState = "qualification_failed"
		}
	}
	for index := range selection.Attempts {
		if selection.Attempts[index].ChannelID == channelID {
			selection.Attempts[index].FilterReason = reason
		}
	}
	selection.Decision.AdmissionState = "qualification_failed"
	if selection.Decision.SelectedChannelID == channelID {
		selection.Decision.SelectedChannelID = 0
	}
	c.Set("route_live_selection", selection)
}

const (
	liveRouteAttemptCountKey       = "route_live_attempt_count"
	liveRouteSameKeyRetryCountKey  = "route_live_same_key_retry_count"
	liveRouteSameRetryCountKey     = "route_live_same_resource_retry_count"
	liveRouteFailoverCountKey      = "route_live_failover_count"
	liveRoutePreviousChannelKey    = "route_live_previous_channel_id"
	liveRoutePreviousKeyIndexKey   = "route_live_previous_key_index"
	liveRouteHasPreviousAttemptKey = "route_live_has_previous_attempt"
	liveRouteRenewalFailedKey      = "route_live_renewal_failed"
)

func liveRouteRetryCounters(c *gin.Context) service.RouteRetryCounters {
	if c == nil {
		return service.RouteRetryCounters{}
	}
	return service.RouteRetryCounters{
		SameKeyAttempts:     c.GetInt(liveRouteSameKeyRetryCountKey),
		SameChannelAttempts: c.GetInt(liveRouteSameRetryCountKey),
		FailoverAttempts:    c.GetInt(liveRouteFailoverCountKey),
		TotalAttempts:       c.GetInt(liveRouteAttemptCountKey),
	}
}

func beginLiveRouteUpstreamAttempt(c *gin.Context, channelID int) int {
	counters := liveRouteRetryCounters(c)
	keyIndex := 0
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		keyIndex = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	if c.GetBool(liveRouteHasPreviousAttemptKey) {
		previousChannelID := c.GetInt(liveRoutePreviousChannelKey)
		previousKeyIndex := c.GetInt(liveRoutePreviousKeyIndexKey)
		switch {
		case previousChannelID != channelID:
			counters.FailoverAttempts++
		case previousKeyIndex == keyIndex:
			counters.SameKeyAttempts++
		default:
			counters.SameChannelAttempts++
		}
	}
	retryIndex := counters.TotalAttempts
	counters.TotalAttempts++
	c.Set(liveRouteAttemptCountKey, counters.TotalAttempts)
	c.Set(liveRouteSameKeyRetryCountKey, counters.SameKeyAttempts)
	c.Set(liveRouteSameRetryCountKey, counters.SameChannelAttempts)
	c.Set(liveRouteFailoverCountKey, counters.FailoverAttempts)
	c.Set(liveRoutePreviousChannelKey, channelID)
	c.Set(liveRoutePreviousKeyIndexKey, keyIndex)
	c.Set(liveRouteHasPreviousAttemptKey, true)
	service.RecordLiveRouteAttemptedKey(c, channelID, keyIndex)
	return retryIndex
}

func nextLiveRouteCandidateIndex(c *gin.Context, currentAttempt int) (int, bool) {
	if c == nil || (c.Request != nil && requestContextDone(c)) {
		return 0, false
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if !ok || !valid {
		return 0, false
	}
	_, index, found := selection.CandidateAtOrAfter(currentAttempt + 1)
	return index, found
}

func nextLiveRouteAttemptForError(c *gin.Context, currentAttempt, currentChannelID int, class service.RouteErrorClassification) (int, bool) {
	if c == nil || (c.Request != nil && requestContextDone(c)) {
		return 0, false
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if !ok || !valid {
		return 0, false
	}
	for {
		candidate, index, found := selection.NextCandidateForError(currentAttempt, currentChannelID, class, liveRouteRetryCounters(c))
		if !found {
			return 0, false
		}
		if reason := knownRouteCandidateUnavailable(c, candidate); reason != "" {
			markLiveRouteCandidateFiltered(c, candidate.ChannelID, reason)
			currentAttempt = index
			continue
		}
		return index, true
	}
}

func relayRetryLimit(c *gin.Context) int {
	if routeLiveSelectionActive(c) {
		if value, ok := c.Get("route_live_selection"); ok {
			if selection, valid := value.(service.LiveRouteSelection); valid && len(selection.Attempts) > 0 {
				return len(selection.Attempts) - 1
			}
		}
		return service.DefaultTotalAttempts - 1
	}
	return common.RetryTimes
}

func waitForLiveRouteBackoff(c *gin.Context, attempt int, retryAfter time.Duration) bool {
	if !routeLiveSelectionActive(c) || c.Request == nil {
		return true
	}
	releaseRouteAttempt(c)
	ctx := c.Request.Context()
	remaining := time.Duration(0)
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
		if remaining <= 0 {
			return false
		}
	}
	if remaining > 0 && retryAfter > remaining {
		return false
	}
	delay := service.RouteBackoff(attempt, retryAfter, remaining, rand.Float64())
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func liveRouteScopedRetryAfter(c *gin.Context, nextAttempt, failedChannelID int, retryAfter time.Duration) time.Duration {
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if ok && valid {
		if next, found := selection.CandidateForAttempt(nextAttempt); found && next.ChannelID != failedChannelID {
			return 0
		}
	}
	return retryAfter
}

func routeLiveSelectionActive(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	return ok && valid && selection.Source != service.RouteSourceUnavailable
}

// liveRoutePriceRatioAllowed applies a manual route policy as a final
// admission ceiling after the established billing helper has calculated the
// actual group ratio. It deliberately does not alter the billing calculation
// or select a different group/channel.
func liveRoutePriceRatioAllowed(c *gin.Context, actualRatio float64) bool {
	if c == nil {
		return true
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	return !ok || !valid || selection.AllowsPriceRatio(actualRatio)
}

const liveRouteRuntimeQualificationContextKey = "route_live_runtime_qualification"

type liveRouteRuntimeQualification struct {
	PriceEligibilityKnown    bool
	PriceEligible            bool
	SecurityEligibilityKnown bool
	SecurityAllowed          bool
}

func markLiveRouteSecurityQualified(c *gin.Context) {
	if c == nil {
		return
	}
	qualification, _ := c.Get(liveRouteRuntimeQualificationContextKey)
	facts, _ := qualification.(liveRouteRuntimeQualification)
	facts.SecurityEligibilityKnown = true
	facts.SecurityAllowed = true
	c.Set(liveRouteRuntimeQualificationContextKey, facts)
}

func markLiveRoutePriceQualified(c *gin.Context) {
	if c == nil {
		return
	}
	qualification, _ := c.Get(liveRouteRuntimeQualificationContextKey)
	facts, _ := qualification.(liveRouteRuntimeQualification)
	facts.PriceEligibilityKnown = true
	facts.PriceEligible = true
	c.Set(liveRouteRuntimeQualificationContextKey, facts)
}

func liveRouteRuntimeQualificationForRequest(c *gin.Context) (liveRouteRuntimeQualification, bool) {
	if c == nil {
		return liveRouteRuntimeQualification{}, false
	}
	value, found := c.Get(liveRouteRuntimeQualificationContextKey)
	facts, valid := value.(liveRouteRuntimeQualification)
	if !found || !valid || !facts.PriceEligibilityKnown || !facts.SecurityEligibilityKnown {
		return liveRouteRuntimeQualification{}, false
	}
	return facts, true
}

func finalizeLiveRouteDecision(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	if c == nil || !routeLiveSelectionActive(c) {
		return
	}
	value, ok := c.Get("route_live_selection")
	selection, valid := value.(service.LiveRouteSelection)
	if !ok || !valid {
		return
	}
	if info != nil {
		counters := liveRouteRetryCounters(c)
		selection.Decision.ScannedCandidates = c.GetInt("gateway_scanned_candidates")
		selection.Decision.UpstreamAttempts = counters.TotalAttempts
		selection.Decision.RetryBudgetExhausted = c.GetBool("gateway_retry_budget_exhausted")
		if policy, budget, ok := service.GatewayAdmissionFromContext(c.Request.Context()); ok {
			selection.Decision.WaitMS = (policy.Wait - budget.Remaining()).Milliseconds()
		}
		if info.ConversionPlan != nil {
			selection.Decision.PlanID = string(info.ConversionPlan.From) + ":" + string(info.ConversionPlan.To) + ":" + string(info.ConversionPlan.Policy)
			if operation := info.ConversionPlan.Operation; operation != "" && operation != "text" {
				selection.Decision.PlanID += ":" + operation
			}
			if info.ConversionPlan.RequestProjection != "" {
				selection.Decision.PlanID += ":" + info.ConversionPlan.RequestProjection
			}
		}
		selection.Decision.ConversionDiagnostics = info.ConversionDiagnostics
		if info.ResponseUsage != nil {
			selection.Decision.UsageSource = info.ResponseUsage.UsageSource
		}
		selection.Decision.HealthEpoch = c.GetInt64("gateway_health_epoch")
		selection.Decision.TokenBudget = c.GetInt64("gateway_token_budget")
		selection.Decision.TokenBudgetSource = c.GetString("gateway_token_budget_source")
		if counters.TotalAttempts > 0 {
			selection.Decision.RetryAttempt = counters.TotalAttempts - 1
			// Later candidate checks may clear or replace the selected channel
			// without sending. Final evidence belongs to the last actual send.
			selection.Decision.SelectedChannelID = c.GetInt(liveRoutePreviousChannelKey)
		}
		selection.Decision.SameResourceRetry = counters.SameChannelAttempts + counters.SameKeyAttempts
		selection.Decision.FailoverAttempt = counters.FailoverAttempts
	}
	if apiErr != nil {
		classification := service.ClassifyRouteError(apiErr.StatusCode, string(apiErr.GetErrorCode()), apiErr.Error(), info != nil && info.HasValidOutput())
		if value, exists := c.Get("gateway_last_attempt_outcome"); exists {
			if outcome, ok := value.(service.AttemptOutcome); ok && !outcome.Success {
				classification = service.ClassifyAttemptOutcome(outcome)
				selection.Decision.RejectionOrigin = outcome.Origin
			}
		}
		selection.Decision.SetFinalError(classification.Class)
	}
	service.EnqueueRouteDecision(selection.Decision)
}

func liveRouteFailoverAttempt(selection service.LiveRouteSelection, attempt int) int {
	if attempt <= 0 {
		return 0
	}
	failovers := 0
	previousChannelID := 0
	for index, candidate := range selection.Attempts {
		if index > attempt {
			break
		}
		if previousChannelID != 0 && candidate.ChannelID != previousChannelID {
			failovers++
		}
		previousChannelID = candidate.ChannelID
	}
	return failovers
}

func channelID(channel *model.Channel) int {
	if channel == nil {
		return 0
	}
	return channel.Id
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

// admitMediaRouteAttempt is called after provider validation and pricing.
func admitMediaRouteAttempt(c *gin.Context, info *relaycommon.RelayInfo, attempt int) error {
	if !liveRoutePriceRatioAllowed(c, info.PriceData.ChannelRatio) {
		return service.ErrRoutePriceRatioExceeded
	}
	channel, err := model.GetChannelById(common.GetContextKeyInt(c, constant.ContextKeyChannelId), true)
	if err != nil {
		return err
	}
	markLiveRouteSecurityQualified(c)
	markLiveRoutePriceQualified(c)
	if err := acquireRouteAttemptPermit(c, info, channel, attempt); err != nil {
		return err
	}
	return nil
}

func RelayMidjourney(c *gin.Context) {
	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"description": "failed to generate relay info", "code": 4})
		return
	}
	var mjErr *taskdto.MidjourneyResponse
	defer func() {
		releaseRouteAttempt(c)
		finalizeLiveRouteDecision(c, info, info.LastError)
		if mjErr != nil && info.Billing != nil {
			info.Billing.Refund(c)
		}
	}()
	switch info.RelayMode {
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, info.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		info.BeforeUpstream = func() error { return admitMediaRouteAttempt(c, info, 0) }
		started := time.Now()
		mjErr = relay.RelayMidjourneyTaskImageSeed(c, info)
		observeLiveRouteAttempt(c, info, relayAttemptOutcome(c, info, info.LastError, started, false))
	default:
		retry := &service.RetryParam{Ctx: c, Retry: common.GetPointer(0)}
		for {
			if routeLiveSelectionActive(c) && liveRouteRetryCounters(c).TotalAttempts >= service.DefaultTotalAttempts {
				break
			}
			releaseRouteAttempt(c)
			channel, channelErr := getChannel(c, info, retry)
			if channelErr != nil {
				if info.LastError == nil {
					mjErr = service.MidjourneyRequestError(info, channelErr, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
				}
				break
			}
			if err := info.ResetAttemptState(); err != nil {
				mjErr = service.MidjourneyRequestError(info, err, types.ErrorCodeUpdateDataError, http.StatusInternalServerError)
				break
			}
			info.LastError = nil
			var admissionErr error
			info.BeforeUpstream = func() error {
				attempt := retry.GetRetry()
				if _, exists := c.Get("gateway_origin_midjourney"); exists {
					attempt = 0
				}
				admissionErr = admitMediaRouteAttempt(c, info, attempt)
				return admissionErr
			}
			started := time.Now()
			if info.RelayMode == relayconstant.RelayModeSwapFace {
				mjErr = relay.RelaySwapFace(c, info)
			} else {
				mjErr = relay.RelayMidjourneySubmit(c, info)
			}
			if mjErr != nil && info.LastError == nil {
				service.MidjourneyRequestError(info, errors.New(mjErr.Description), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
			}
			outcome := relayAttemptOutcome(c, info, info.LastError, started, true)
			observeLiveRouteAttempt(c, info, outcome)
			if mjErr == nil {
				break
			}
			_, bound := c.Get("gateway_origin_midjourney")
			if admissionErr != nil && service.LiveRouteQualificationAllowsFailover(admissionErr) {
				reason := "admission_failed"
				if errors.Is(admissionErr, service.ErrAdmissionCapacity) {
					reason = "capacity_exhausted"
				}
				markLiveRouteCandidateFiltered(c, info.ChannelId, reason)
				if !bound {
					if next, found := nextLiveRouteCandidateIndex(c, retry.GetRetry()); found {
						retry.SetRetry(next)
						continue
					}
				}
				if next, found := queueBusyLiveRouteCandidate(c); found {
					retry.SetRetry(next)
					continue
				}
			}
			// A stateful follow-up may queue on its owner but cannot switch it.
			if bound {
				break
			}
			if markLiveRoutePreSendRejection(c, info, info.LastError) {
				if next, found := nextLiveRouteCandidateIndex(c, retry.GetRetry()); found {
					retry.SetRetry(next)
					continue
				}
				if next, found := queueBusyLiveRouteCandidate(c); found {
					retry.SetRetry(next)
					continue
				}
				break
			}
			// Unknown acceptance is never replayed. Explicit proxy rejections
			// may use the existing common error policy and three-send budget.
			classification := service.ClassifyAttemptOutcome(outcome)
			if routeLiveSelectionActive(c) && service.CanRouteFailover(classification, relayResponseCommitted(c, info), false) {
				if next, found := nextLiveRouteAttemptForError(c, retry.GetRetry(), channel.Id, classification); found {
					if !waitForLiveRouteBackoff(c, info.RetryIndex, liveRouteScopedRetryAfter(c, next, channel.Id, outcome.RetryAfter)) {
						break
					}
					retry.SetRetry(next)
					continue
				}
			}
			break
		}
	}
	if mjErr != nil && !c.Writer.Written() && !requestContextDone(c) {
		status := http.StatusBadRequest
		if info.LastError != nil {
			status = info.LastError.StatusCode
		} else if mjErr.Description == "token_model_forbidden" {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"description": mjErr.Description, "type": "midjourney_error", "code": mjErr.Code})
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	if locked, ok := relayInfo.LockedChannel.(*model.Channel); ok && locked != nil {
		if _, routeErr := middleware.SelectRequestChannel(c, relayInfo.OriginModelName, locked.Id, relaycommon.CompactAttemptNone); routeErr != nil {
			respondTaskError(c, service.TaskErrorFromAPIError(routeErr))
			return
		}
		relayInfo.InitChannelMeta(c)
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	defer func() {
		releaseRouteAttempt(c)
		finalizeLiveRouteDecision(c, relayInfo, service.APIErrorFromTaskError(taskErr))
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:   c,
		Retry: common.GetPointer(0),
	}

	for {
		if (routeLiveSelectionActive(c) && liveRouteRetryCounters(c).TotalAttempts >= service.DefaultTotalAttempts) ||
			(!routeLiveSelectionActive(c) && retryParam.GetRetry() > relayRetryLimit(c)) {
			break
		}
		if _, exists := c.Get("route_channel_permit"); exists {
			releaseRouteAttempt(c)
			if routeLiveSelectionActive(c) && c.GetBool(liveRouteRenewalFailedKey) {
				break
			}
		}
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			if taskErr == nil || !errors.Is(channelErr, service.ErrRouteSelectionUnavailable) {
				taskErr = service.TaskErrorWrapperLocal(channelErr, "get_channel_failed", http.StatusServiceUnavailable)
			}
			break
		}
		if resetErr := relayInfo.ResetAttemptState(); resetErr != nil {
			taskErr = service.TaskErrorWrapperLocal(resetErr, "attempt_reset_failed", http.StatusInternalServerError)
			break
		}
		var admissionErr error
		relayInfo.BeforeUpstream = func() error {
			admissionErr = admitMediaRouteAttempt(c, relayInfo, retryParam.GetRetry())
			return admissionErr
		}
		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		attemptStartedAt := time.Now()
		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		attemptError := service.APIErrorFromTaskError(taskErr)
		outcome := relayAttemptOutcome(c, relayInfo, attemptError, attemptStartedAt, true)
		observeLiveRouteAttempt(c, relayInfo, outcome)
		if admissionErr != nil {
			if service.LiveRouteQualificationAllowsFailover(admissionErr) {
				reason := "admission_failed"
				if errors.Is(admissionErr, service.ErrAdmissionCapacity) {
					reason = "capacity_exhausted"
				}
				markLiveRouteCandidateFiltered(c, channel.Id, reason)
				if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
				if nextAttempt, found := queueBusyLiveRouteCandidate(c); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
			}
			break
		}
		if taskErr == nil {
			break
		}
		if markLiveRoutePreSendRejection(c, relayInfo, attemptError) {
			if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			if nextAttempt, found := queueBusyLiveRouteCandidate(c); found {
				retryParam.SetRetry(nextAttempt)
				continue
			}
			break
		}

		if routeLiveSelectionActive(c) {
			if taskErr.Code == "route_price_ratio_exceeded" {
				markLiveRouteCandidateFiltered(c, channel.Id, service.RouteFilterPriceForbidden)
				if nextAttempt, found := nextLiveRouteCandidateIndex(c, retryParam.GetRetry()); found {
					retryParam.SetRetry(nextAttempt)
					continue
				}
				break
			}
			classification := service.ClassifyAttemptOutcome(outcome)
			if !taskErr.LocalError && service.CanRouteFailover(classification, relayResponseCommitted(c, relayInfo), false) {
				if nextAttempt, found := nextLiveRouteAttemptForError(c, retryParam.GetRetry(), channel.Id, classification); found {
					if !waitForLiveRouteBackoff(c, retryParam.GetRetry(), liveRouteScopedRetryAfter(c, nextAttempt, channel.Id, outcome.RetryAfter)) {
						break
					}
					retryParam.SetRetry(nextAttempt)
					continue
				}
			}
		}

		if !taskErr.LocalError {
			channelError := types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
				common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			if routeLiveSelectionActive(c) {
				channelError.AutoBan = false
			}
			processChannelError(c,
				*channelError,
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if routeLiveSelectionActive(c) {
			break
		}
		break
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	if taskErr == nil {
		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData = result.Credential
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.ChannelRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios(),
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if persistErr := service.FinalizeTaskSubmission(c, relayInfo, task); persistErr != nil {
			common.SysError("persist accepted task error: " + persistErr.Error())
			taskErr = service.TaskErrorWrapperLocal(errors.New("accepted task could not be saved; do not resubmit automatically"), "task_persistence_failed", http.StatusServiceUnavailable)
		} else {
			relayInfo.MarkValidOutput()
			c.JSON(http.StatusOK, result.ClientResponse)
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if requestContextDone(c) || c.Writer.Written() {
		return
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func requestContextDone(c *gin.Context) bool {
	return c == nil || c.Request == nil || c.Request.Context().Err() != nil
}
