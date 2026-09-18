package oaichat

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func AppendHostedClaudeBlock(info convmeta.Meta, block dto.ClaudeMediaMessage) []*dto.ClaudeResponse {
	if info == nil {
		return nil
	}
	state := info.EnsureClaudeConvertInfo()
	if state.Done {
		return nil
	}
	var events []*dto.ClaudeResponse
	if !state.MessageStarted {
		events = append(events, StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{Model: info.GetUpstreamModelName(), Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}}}}, info)...)
	}
	events = append(events, stopOpenBlocks(state)...)
	if state.LastMessagesType == convmeta.LastMessageTypeTools {
		state.Index = state.ToolCallBaseIndex + state.ToolCallMaxIndexOffset + 1
	} else if state.LastMessagesType != convmeta.LastMessageTypeNone {
		state.Index++
	}
	state.LastMessagesType = convmeta.LastMessageTypeNone
	index := state.Index
	events = append(events, &dto.ClaudeResponse{Type: "content_block_start", Index: kitutil.GetPointer(index), ContentBlock: &block}, generateStopBlock(index))
	state.Index++
	return events
}
