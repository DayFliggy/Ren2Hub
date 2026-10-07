package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

type realtimeMessage struct {
	fromClient bool
	data       []byte
	err        error
}

type realtimeResponseState struct {
	estimate  *dto.RealtimeUsage
	delivered bool
	items     map[string]*dto.RealtimeUsage
}

// Readers only read/enqueue; usage and RelayInfo are mutated by this loop.
func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (apiErr *types.NewAPIError, total *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return realtimeRelayError(fmt.Errorf("invalid websocket connection")), nil
	}
	info.IsStream = true
	info.StreamStatus = relaycommon.NewStreamStatus()
	defer func() {
		if apiErr == nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
		} else if errors.Is(apiErr, context.Canceled) {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, apiErr)
		} else {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, apiErr)
		}
	}()
	clientConn, targetConn := info.ClientWs, info.TargetWs
	messages := make(chan realtimeMessage, 2)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	read := func(conn *websocket.Conn, fromClient bool) {
		defer readers.Done()
		for {
			_, data, err := conn.ReadMessage()
			select {
			case messages <- realtimeMessage{fromClient: fromClient, data: data, err: err}:
			case <-stop:
				return
			}
			if err != nil {
				return
			}
		}
	}
	readers.Add(2)
	go read(clientConn, true)
	go read(targetConn, false)
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(c.Request.Context(), func() {
		_ = clientConn.Close()
		_ = targetConn.Close()
		close(cancelDone)
	})
	defer func() {
		close(stop)
		_ = clientConn.Close()
		_ = targetConn.Close()
		readers.Wait()
		if !stopCancel() {
			<-cancelDone
		}
	}()

	total = &dto.RealtimeUsage{}
	pendingInput := &dto.RealtimeUsage{}
	activeResponses := make(map[string]*realtimeResponseState)
	seenResponses := make(map[string]string)
	pendingCreates, pendingBillableInput := 0, false
	providerUsage, estimatedUsage := false, false
	uncertainOutcome := false
	defer func() {
		for _, response := range activeResponses {
			if !response.delivered {
				continue
			}
			merged, err := service.AddRealtimeUsage(total, response.estimate)
			if err != nil {
				if apiErr == nil {
					apiErr = realtimeRelayError(err)
				}
				break
			}
			total = merged
			estimatedUsage = estimatedUsage || response.estimate.TotalTokens > 0
		}
		source := "estimated"
		if providerUsage {
			source = "provider"
			if estimatedUsage {
				source = "provider_and_estimated"
			}
		}
		ids := make([]string, 0, len(seenResponses))
		for id := range seenResponses {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		complete := apiErr == nil && len(activeResponses) == 0 && pendingCreates == 0 && !pendingBillableInput && providerUsage && !estimatedUsage && !uncertainOutcome
		terminal := "unknown"
		if complete {
			terminal = "completed"
		}
		total.Observation = &dto.UsageObservation{Source: source, Estimated: estimatedUsage || !providerUsage,
			Complete: complete, Terminal: terminal, ResponseIDs: ids}
		info.CaptureResponseUsage(total.CanonicalUsage(), "realtime", nil)
	}()
	for {
		select {
		case <-c.Request.Context().Done():
			return realtimeClientError(c.Request.Context().Err()), total
		case msg := <-messages:
			if msg.err != nil {
				normalClose := websocket.IsCloseError(msg.err, websocket.CloseNormalClosure, websocket.CloseGoingAway)
				if c.Request.Context().Err() != nil || msg.fromClient && !normalClose {
					return realtimeClientError(context.Canceled), total
				}
				if normalClose && len(activeResponses) == 0 && (msg.fromClient || len(seenResponses) > 0) {
					peer := clientConn
					if msg.fromClient {
						peer = targetConn
					}
					if err := peer.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)); err != nil && !msg.fromClient {
						return realtimeClientError(context.Canceled), total
					}
					return nil, total
				}
				if msg.fromClient {
					return realtimeClientError(context.Canceled), total
				}
				logger.LogError(c, "upstream realtime websocket disconnected")
				if normalClose {
					return realtimeRelayError(io.ErrUnexpectedEOF), total
				}
				return realtimeRelayError(msg.err), total
			}
			var event dto.RealtimeEvent
			if err := common.Unmarshal(msg.data, &event); err != nil {
				if msg.fromClient {
					return realtimeClientError(fmt.Errorf("decode realtime event: %w", err)), total
				}
				return realtimeRelayError(fmt.Errorf("decode realtime event: %w", err)), total
			}
			if event.Type == "" {
				if msg.fromClient {
					return realtimeClientError(errors.New("realtime event type is missing")), total
				}
				return realtimeRelayError(errors.New("upstream realtime event type is missing")), total
			}
			if msg.fromClient {
				if err := helper.ValidateRealtimeBillingEvent(info, msg.data); err != nil {
					return helper.BillingRequestAPIError(err), total
				}
				if event.Type == dto.RealtimeEventTypeSessionUpdate && event.Session != nil && event.Session.Tools != nil {
					info.RealtimeTools = event.Session.Tools
				}
				addition, err := realtimeEventUsage(info, event, true)
				if err != nil {
					return realtimeClientError(err), total
				}
				updated, err := service.AddRealtimeUsage(pendingInput, addition)
				if err != nil {
					return realtimeClientError(err), total
				}
				if err := writeRealtimeMessage(c, targetConn, msg.data); err != nil {
					return realtimeRelayError(err), total
				}
				pendingInput = updated
				switch event.Type {
				case dto.RealtimeEventTypeResponseCreate:
					pendingCreates++
				case dto.RealtimeEventInputAudioBufferAppend, "input_audio_buffer.commit", dto.RealtimeEventTypeConversationCreate:
					pendingBillableInput = true
				}
				continue
			}

			reserveUsage := false
			var responseErr *types.NewAPIError
			var responseState *realtimeResponseState
			var completedUsage *dto.RealtimeUsage
			var itemUsage *dto.RealtimeUsage
			var itemID string
			var responseID string
			var responseEvidence string
			terminal := event.Type == dto.RealtimeEventTypeResponseDone
			responseEvent := strings.HasPrefix(event.Type, "response.")
			semantic := responseEvent && event.Delta != ""
			duplicate := false
			if responseEvent {
				responseID = gjson.GetBytes(msg.data, "response_id").String()
				if id := gjson.GetBytes(msg.data, "response.id").String(); id != "" {
					responseID = id
				}
				if responseID == "" {
					if len(activeResponses) > 1 {
						return realtimeRelayError(errors.New("realtime event is missing response identity during concurrent responses")), total
					}
					for id := range activeResponses {
						responseID = id
					}
				}
				_, duplicate = seenResponses[responseID]
				if duplicate && !terminal {
					return realtimeRelayError(errors.New("realtime event follows a completed response")), total
				}
				if !duplicate {
					responseState = activeResponses[responseID]
					// Older compatible streams omit the ID on deltas. Associate
					// them only while there is a single unambiguous response.
					if responseState == nil && event.Type != "response.created" && len(activeResponses) == 1 && activeResponses[""] != nil {
						responseState = activeResponses[""]
						delete(activeResponses, "")
					}
					if responseState == nil {
						responseState = &realtimeResponseState{estimate: pendingInput, items: make(map[string]*dto.RealtimeUsage)}
						pendingInput = &dto.RealtimeUsage{}
						pendingCreates = max(0, pendingCreates-1)
						pendingBillableInput = false
					}
					activeResponses[responseID] = responseState
				}
			}
			switch event.Type {
			case dto.RealtimeEventTypeSessionUpdated, dto.RealtimeEventTypeSessionCreated:
				if event.Session != nil {
					info.InputAudioFormat = common.GetStringIfEmpty(event.Session.InputAudioFormat, info.InputAudioFormat)
					info.OutputAudioFormat = common.GetStringIfEmpty(event.Session.OutputAudioFormat, info.OutputAudioFormat)
				}
			case dto.RealtimeEventTypeResponseDone:
				var identity struct {
					Response *struct {
						ID            string `json:"id"`
						Status        string `json:"status"`
						StatusDetails struct {
							Error *types.OpenAIError `json:"error"`
						} `json:"status_details"`
					} `json:"response"`
				}
				if err := common.Unmarshal(msg.data, &identity); err != nil {
					return realtimeRelayError(err), total
				}
				if event.Response == nil {
					return realtimeRelayError(fmt.Errorf("realtime response.done is missing response")), total
				}
				if identity.Response.ID == "" {
					return realtimeRelayError(errors.New("realtime response.done is missing response identity")), total
				}
				evidence, err := common.Marshal(struct {
					Status   string             `json:"status"`
					Usage    *dto.RealtimeUsage `json:"usage"`
					Complete bool               `json:"complete"`
				}{identity.Response.Status, event.Response.Usage, event.Response.Usage != nil && (event.Response.Usage.Observation == nil || event.Response.Usage.Observation.Complete)})
				if err != nil {
					return realtimeRelayError(err), total
				}
				responseEvidence = string(evidence)
				if duplicate && seenResponses[responseID] != responseEvidence {
					return realtimeRelayError(errors.New("conflicting usage for completed realtime response")), total
				}
				switch identity.Response.Status {
				case "", "completed", "cancelled", "incomplete":
				case "failed":
					responseErr = realtimeProviderError(identity.Response.StatusDetails.Error)
				default:
					return realtimeRelayError(fmt.Errorf("invalid realtime response.done status")), total
				}
				if !duplicate {
					uncertainOutcome = uncertainOutcome || identity.Response.Status != "completed"
					finalOutput, hasOutput, err := realtimeTerminalOutputUsage(info, msg.data)
					if err != nil {
						return realtimeRelayError(err), total
					}
					semantic = hasOutput
					completed := responseState.estimate
					if event.Response.Usage != nil {
						if observation := event.Response.Usage.Observation; observation != nil && !observation.Complete {
							uncertainOutcome = true
						}
						var err error
						completed, err = service.NormalizeRealtimeUsage(event.Response.Usage)
						if err != nil {
							return realtimeRelayError(err), total
						}
					} else {
						// The terminal contains complete output values. Reconcile
						// them with delivered deltas without counting both copies.
						estimate := *completed
						estimate.OutputTokenDetails.TextTokens = max(estimate.OutputTokenDetails.TextTokens, finalOutput.OutputTokenDetails.TextTokens)
						estimate.OutputTokenDetails.AudioTokens = max(estimate.OutputTokenDetails.AudioTokens, finalOutput.OutputTokenDetails.AudioTokens)
						estimate.OutputTokens = estimate.OutputTokenDetails.TextTokens + estimate.OutputTokenDetails.AudioTokens
						estimate.TotalTokens = estimate.InputTokens + estimate.OutputTokens
						completed = &estimate
						addition, err := realtimeEventUsage(info, event, true)
						if err != nil {
							return realtimeRelayError(err), total
						}
						completed, err = service.AddRealtimeUsage(completed, addition)
						if err != nil {
							return realtimeRelayError(err), total
						}
					}
					completedUsage = completed
				}
			default:
				addition, err := realtimeEventUsage(info, event, false)
				if err != nil {
					return realtimeRelayError(err), total
				}
				base := pendingInput
				if responseState != nil {
					base = responseState.estimate
					itemID = gjson.GetBytes(msg.data, "item_id").String()
					if id := gjson.GetBytes(msg.data, "item.id").String(); id != "" {
						itemID = id
					}
					if itemID == "" {
						if index := gjson.GetBytes(msg.data, "output_index"); index.Exists() {
							itemID = "index:" + index.String()
						}
					}
					previous := responseState.items[itemID]
					if previous == nil {
						previous = &dto.RealtimeUsage{}
					}
					if event.Type == "response.output_item.done" {
						finalOutput, hasOutput, err := realtimeOutputItemsUsage(info, []gjson.Result{gjson.GetBytes(msg.data, "item")})
						if err != nil {
							return realtimeRelayError(err), total
						}
						semantic = hasOutput
						// Full values replace the item's delivered deltas, including
						// duplicate item.done events, without replacing other items.
						addition.OutputTokenDetails.TextTokens = max(0, finalOutput.OutputTokenDetails.TextTokens-previous.OutputTokenDetails.TextTokens)
						addition.OutputTokenDetails.AudioTokens = max(0, finalOutput.OutputTokenDetails.AudioTokens-previous.OutputTokenDetails.AudioTokens)
						addition, err = service.NormalizeRealtimeUsage(addition)
						if err != nil {
							return realtimeRelayError(err), total
						}
					}
					itemUsage, err = service.AddRealtimeUsage(previous, addition)
					if err != nil {
						return realtimeRelayError(err), total
					}
				}
				updated, err := service.AddRealtimeUsage(base, addition)
				if err != nil {
					return realtimeRelayError(err), total
				}
				if responseState != nil {
					// Commit this estimate only after the client received the event.
					completedUsage = updated
				} else {
					pendingInput = updated
				}
			}
			if err := writeRealtimeMessage(c, clientConn, msg.data); err != nil {
				return realtimeClientError(context.Canceled), total
			}
			if semantic {
				info.SetFirstResponseTime()
				info.MarkValidOutput()
				if responseState != nil {
					responseState.delivered = true
				}
			}
			if terminal && !duplicate {
				if responseState.delivered || event.Response.Usage != nil {
					merged, err := service.AddRealtimeUsage(total, completedUsage)
					if err != nil {
						return realtimeRelayError(err), total
					}
					total = merged
					providerUsage = providerUsage || event.Response.Usage != nil
					estimatedUsage = estimatedUsage || event.Response.Usage == nil
					reserveUsage = true
				}
				delete(activeResponses, responseID)
				seenResponses[responseID] = responseEvidence
				info.IsFirstRequest = false
			} else if responseState != nil && completedUsage != nil {
				responseState.estimate = completedUsage
				responseState.items[itemID] = itemUsage
			}
			if event.Type == dto.RealtimeEventTypeError {
				responseErr = realtimeProviderError(event.Error)
			}
			if responseErr != nil {
				return responseErr, total
			}
			if reserveUsage {
				total.Observation = &dto.UsageObservation{Source: "realtime_cumulative", Estimated: estimatedUsage, Terminal: "collecting"}
				if err := service.PreWssConsumeQuota(c, info, total); err != nil {
					return types.NewErrorWithStatusCode(fmt.Errorf("reserve realtime usage: %w", err), types.ErrorCodeUpdateDataError,
						http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry()), total
				}
			}
		}
	}
}

func realtimeTerminalOutputUsage(info *relaycommon.RelayInfo, data []byte) (*dto.RealtimeUsage, bool, error) {
	return realtimeOutputItemsUsage(info, gjson.GetBytes(data, "response.output").Array())
}

func realtimeOutputItemsUsage(info *relaycommon.RelayInfo, items []gjson.Result) (*dto.RealtimeUsage, bool, error) {
	usage := &dto.RealtimeUsage{}
	hasOutput := false
	for _, item := range items {
		var text strings.Builder
		if item.Get("type").String() == "function_call" && item.Get("name").String() != "" {
			hasOutput = true
			text.WriteString(item.Get("name").String())
			text.WriteString(item.Get("arguments").String())
		}
		for _, content := range item.Get("content").Array() {
			value := content.Get("text").String()
			if value == "" {
				value = content.Get("transcript").String()
			}
			text.WriteString(value)
			hasOutput = hasOutput || value != ""
			if audio := content.Get("audio").String(); audio != "" {
				addition, err := realtimeEventUsage(info, dto.RealtimeEvent{Type: dto.RealtimeEventResponseAudioDelta, Delta: audio}, false)
				if err != nil {
					return nil, false, err
				}
				usage, err = service.AddRealtimeUsage(usage, addition)
				if err != nil {
					return nil, false, err
				}
				hasOutput = true
			}
		}
		textUsage := &dto.RealtimeUsage{OutputTokenDetails: dto.OutputTokenDetails{TextTokens: tokenkit.Count(info.UpstreamModelName, text.String())}}
		var err error
		usage, err = service.AddRealtimeUsage(usage, textUsage)
		if err != nil {
			return nil, false, err
		}
	}
	return usage, hasOutput, nil
}

func realtimeEventUsage(info *relaycommon.RelayInfo, event dto.RealtimeEvent, input bool) (*dto.RealtimeUsage, error) {
	if event.Type == dto.RealtimeEventConversationItemCreated {
		input = true
	}
	switch event.Type {
	case "response.text.delta", "response.output_text.delta", "response.output_audio_transcript.delta":
		event.Type = dto.RealtimeEventResponseAudioTranscriptionDelta
	case "response.output_audio.delta":
		event.Type = dto.RealtimeEventResponseAudioDelta
	}
	textTokens, audioTokens, err := service.CountTokenRealtime(info, event, info.UpstreamModelName)
	if err != nil {
		return nil, err
	}
	usage := &dto.RealtimeUsage{}
	if input {
		usage.InputTokenDetails.TextTokens = textTokens
		usage.InputTokenDetails.AudioTokens = audioTokens
	} else {
		usage.OutputTokenDetails.TextTokens = textTokens
		usage.OutputTokenDetails.AudioTokens = audioTokens
	}
	return service.NormalizeRealtimeUsage(usage)
}

func writeRealtimeMessage(c *gin.Context, conn *websocket.Conn, message []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	return helper.WssString(c, conn, string(message))
}

func realtimeRelayError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func realtimeClientError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func realtimeProviderError(detail *types.OpenAIError) *types.NewAPIError {
	if detail == nil {
		return realtimeRelayError(errors.New("upstream realtime response failed"))
	}
	if detail.Type == "invalid_request_error" {
		return realtimeClientError(errors.New(detail.Message))
	}
	status := http.StatusBadGateway
	if detail.Type == "rate_limit_error" || strings.Contains(fmt.Sprint(detail.Code), "rate_limit") {
		status = http.StatusTooManyRequests
	}
	return types.WithOpenAIError(*detail, status, types.ErrOptionWithSkipRetry())
}
