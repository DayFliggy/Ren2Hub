package openai

import (
	"context"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if c == nil || c.Request == nil || info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}
	info.IsStream = true
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	state := &realtimeRelayState{info: info}
	results := make(chan error, 2)
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		results <- forwardRealtime(ctx, info.ClientWs, info.TargetWs, func(event *dto.RealtimeEvent) error {
			return state.recordClientEvent(event)
		})
	}()
	go func() {
		defer readers.Done()
		results <- forwardRealtime(ctx, info.TargetWs, info.ClientWs, func(event *dto.RealtimeEvent) error {
			return state.recordTargetEvent(c, event)
		})
	}()

	select {
	case err := <-results:
		if err != nil && ctx.Err() == nil {
			logger.LogError(c, "realtime error: "+err.Error())
		}
	case <-ctx.Done():
	}
	// Closing both sockets interrupts blocked ReadMessage and WriteMessage.
	// Neither handler completion nor final billing may race a reader's usage.
	cancel()
	_ = info.ClientWs.Close()
	_ = info.TargetWs.Close()
	readers.Wait()
	if state.local.TotalTokens != 0 {
		if err := preConsumeUsage(c, info, &state.local, &state.total); err != nil {
			logger.LogError(c, "realtime final estimated usage consume failed: "+err.Error())
		}
	}
	return nil, &state.total
}

func forwardRealtime(ctx context.Context, source, target *websocket.Conn, record func(*dto.RealtimeEvent) error) (result error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = fmt.Errorf("panic in realtime reader: %v", recovered)
		}
	}()
	for ctx.Err() == nil {
		_, message, err := source.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return fmt.Errorf("read realtime event: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var event dto.RealtimeEvent
		if err := common.Unmarshal(message, &event); err != nil {
			return fmt.Errorf("decode realtime event: %w", err)
		}
		if err := record(&event); err != nil {
			return err
		}
		if err := target.WriteMessage(websocket.TextMessage, message); err != nil {
			return fmt.Errorf("forward realtime event: %w", err)
		}
	}
	return ctx.Err()
}

type realtimeRelayState struct {
	mu    sync.Mutex
	info  *relaycommon.RelayInfo
	local dto.RealtimeUsage
	total dto.RealtimeUsage
}

func (s *realtimeRelayState) recordClientEvent(event *dto.RealtimeEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Type == dto.RealtimeEventTypeSessionUpdate && event.Session != nil && event.Session.Tools != nil {
		s.info.RealtimeTools = event.Session.Tools
	}
	textTokens, audioTokens, err := service.CountTokenRealtime(s.info, *event, s.info.UpstreamModelName)
	if err != nil {
		return fmt.Errorf("count client realtime tokens: %w", err)
	}
	s.local.TotalTokens += textTokens + audioTokens
	s.local.InputTokens += textTokens + audioTokens
	s.local.InputTokenDetails.TextTokens += textTokens
	s.local.InputTokenDetails.AudioTokens += audioTokens
	return nil
}

func (s *realtimeRelayState) recordTargetEvent(c *gin.Context, event *dto.RealtimeEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info.SetFirstResponseTime()
	switch event.Type {
	case dto.RealtimeEventTypeResponseDone:
		if event.Response == nil {
			return fmt.Errorf("realtime response.done is missing response")
		}
		if event.Response.Usage != nil {
			// An attempted debit may have partially applied. Never retry it in
			// shutdown or also bill the local estimate for this same response.
			s.local = dto.RealtimeUsage{}
			if err := preConsumeUsage(c, s.info, event.Response.Usage, &s.total); err != nil {
				return fmt.Errorf("consume realtime usage: %w", err)
			}
			return nil
		}
		textTokens, audioTokens, err := service.CountTokenRealtime(s.info, *event, s.info.UpstreamModelName)
		if err != nil {
			return fmt.Errorf("count realtime response tokens: %w", err)
		}
		s.info.IsFirstRequest = false
		s.local.TotalTokens += textTokens + audioTokens
		s.local.InputTokens += textTokens + audioTokens
		s.local.InputTokenDetails.TextTokens += textTokens
		s.local.InputTokenDetails.AudioTokens += audioTokens
		usage := s.local
		s.local = dto.RealtimeUsage{}
		if err := preConsumeUsage(c, s.info, &usage, &s.total); err != nil {
			return fmt.Errorf("consume estimated realtime usage: %w", err)
		}
		return nil
	case dto.RealtimeEventTypeSessionUpdated, dto.RealtimeEventTypeSessionCreated:
		if event.Session != nil {
			s.info.InputAudioFormat = common.GetStringIfEmpty(event.Session.InputAudioFormat, s.info.InputAudioFormat)
			s.info.OutputAudioFormat = common.GetStringIfEmpty(event.Session.OutputAudioFormat, s.info.OutputAudioFormat)
		}
		return nil
	default:
		textTokens, audioTokens, err := service.CountTokenRealtime(s.info, *event, s.info.UpstreamModelName)
		if err != nil {
			return fmt.Errorf("count upstream realtime tokens: %w", err)
		}
		s.local.TotalTokens += textTokens + audioTokens
		s.local.OutputTokens += textTokens + audioTokens
		s.local.OutputTokenDetails.TextTokens += textTokens
		s.local.OutputTokenDetails.AudioTokens += audioTokens
		return nil
	}
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	totalUsage.TotalTokens += usage.TotalTokens
	totalUsage.InputTokens += usage.InputTokens
	totalUsage.OutputTokens += usage.OutputTokens
	totalUsage.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	totalUsage.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	totalUsage.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	totalUsage.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	totalUsage.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	return service.PreWssConsumeQuota(ctx, info, usage)
}
