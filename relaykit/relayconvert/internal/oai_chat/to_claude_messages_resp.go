package oaichat

import (
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/reasonmap"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
)

func generateStopBlock(index int) *dto.ClaudeResponse {
	return &dto.ClaudeResponse{
		Type:  "content_block_stop",
		Index: kitutil.GetPointer[int](index),
	}
}

func stopOpenBlocks(state *convmeta.ClaudeConvertInfo) []*dto.ClaudeResponse {
	if state == nil {
		return nil
	}
	switch state.LastMessagesType {
	case convmeta.LastMessageTypeText, convmeta.LastMessageTypeThinking:
		return []*dto.ClaudeResponse{generateStopBlock(state.Index)}
	case convmeta.LastMessageTypeTools:
		responses := make([]*dto.ClaudeResponse, 0, state.ToolCallMaxIndexOffset+1)
		for offset := 0; offset <= state.ToolCallMaxIndexOffset; offset++ {
			responses = append(responses, generateStopBlock(state.ToolCallBaseIndex+offset))
		}
		return responses
	default:
		return nil
	}
}

func buildClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	if oaiUsage == nil {
		return nil
	}
	if billingUsage := dto.CloneBillingUsage(oaiUsage.BillingUsage); billingUsage != nil && billingUsage.ClaudeUsage != nil {
		if billingUsage.Source == dto.BillingUsageSourceClaudeMessages || billingUsage.Semantic == dto.BillingUsageSemanticAnthropic {
			return billingUsage.ClaudeUsage
		}
	}
	billingUsage := dto.NewOpenAIChatBillingUsage(oaiUsage)
	if existingBillingUsage := dto.CloneBillingUsage(oaiUsage.BillingUsage); existingBillingUsage != nil && existingBillingUsage.OpenAIUsage != nil {
		if existingBillingUsage.Source == dto.BillingUsageSourceOAIChat ||
			existingBillingUsage.Source == dto.BillingUsageSourceOAIResponses ||
			existingBillingUsage.Semantic == dto.BillingUsageSemanticOpenAI {
			billingUsage = existingBillingUsage
		}
	}
	cacheCreation5m, cacheCreation1h := NormalizeCacheCreationSplit(
		oaiUsage.PromptTokensDetails.CachedCreationTokens,
		oaiUsage.ClaudeCacheCreation5mTokens,
		oaiUsage.ClaudeCacheCreation1hTokens,
	)
	cacheCreationTokens := oaiUsage.PromptTokensDetails.CacheCreationTokensTotal()
	inputTokens := oaiUsage.PromptTokens
	if oaiUsage.PromptTokensDetails.CacheWriteTokens > 0 {
		// OpenAI native cache-write usage counts cached and cache-write tokens
		// inside prompt_tokens, while Claude semantics reports input_tokens
		// excluding both. Both counts are unadjusted prefixes and may overlap,
		// so clamp a negative remainder at zero.
		inputTokens = oaiUsage.PromptTokens - oaiUsage.PromptTokensDetails.CachedTokens - cacheCreationTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
	}
	usage := &dto.ClaudeUsage{
		InputTokens:              inputTokens,
		OutputTokens:             oaiUsage.CompletionTokens,
		CacheCreationInputTokens: cacheCreationTokens,
		CacheReadInputTokens:     oaiUsage.PromptTokensDetails.CachedTokens,
		BillingUsage:             billingUsage,
	}
	if cacheCreation5m > 0 || cacheCreation1h > 0 {
		usage.CacheCreation = &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: cacheCreation5m,
			Ephemeral1hInputTokens: cacheCreation1h,
		}
	}
	return usage
}

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	remainder := lo.Max([]int{totalTokens - tokens5m - tokens1h, 0})
	return tokens5m + remainder, tokens1h
}

func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info convmeta.Meta) []*dto.ClaudeResponse {
	if info == nil {
		info = &convmeta.Values{}
	}
	state := info.EnsureClaudeConvertInfo()
	if state.Done {
		return nil
	}

	var claudeResponses []*dto.ClaudeResponse
	// stopOpenBlocks emits the required content_block_stop event(s) for the currently open block(s)
	// according to Anthropic's SSE streaming state machine:
	// content_block_start -> content_block_delta* -> content_block_stop (per index).
	//
	// For text/thinking, there is at most one open block at state.Index.
	// For tools, OpenAI tool_calls can stream multiple parallel tool_use blocks (indexed from 0),
	// so we may have multiple open blocks and must stop each one explicitly.
	appendStopOpenBlocks := func() {
		claudeResponses = append(claudeResponses, stopOpenBlocks(state)...)
	}
	// stopOpenBlocksAndAdvance closes the currently open block(s) and advances the content block index
	// to the next available slot for subsequent content_block_start events.
	//
	// This prevents invalid streams where a content_block_delta (e.g. thinking_delta) is emitted for an
	// index whose active content_block type is different (the typical cause of "Mismatched content block type").
	stopOpenBlocksAndAdvance := func() {
		if state.LastMessagesType == convmeta.LastMessageTypeNone {
			return
		}
		appendStopOpenBlocks()
		switch state.LastMessagesType {
		case convmeta.LastMessageTypeTools:
			state.Index = state.ToolCallBaseIndex + state.ToolCallMaxIndexOffset + 1
			state.ToolCallBaseIndex = 0
			state.ToolCallMaxIndexOffset = 0
		default:
			state.Index++
		}
		state.LastMessagesType = convmeta.LastMessageTypeNone
	}
	if !state.MessageStarted {
		state.MessageStarted = true
		msg := &dto.ClaudeMediaMessage{
			Id:    openAIResponse.Id,
			Model: openAIResponse.Model,
			Type:  "message",
			Role:  "assistant",
			Usage: &dto.ClaudeUsage{
				InputTokens:  info.GetEstimatePromptTokens(),
				OutputTokens: 0,
			},
		}
		msg.SetContent(make([]any, 0))
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Type:    "message_start",
			Message: msg,
		})
	}

	if len(openAIResponse.Choices) == 0 {
		// Some OpenAI-compatible upstreams end with a usage-only SSE chunk.
		oaiUsage := openAIResponse.Usage
		if oaiUsage == nil {
			oaiUsage = state.Usage
		}
		if oaiUsage != nil {
			appendStopOpenBlocks()
			stopReason := stopReasonOpenAI2Claude(state.FinishReason)
			if stopReason == "" {
				stopReason = "end_turn"
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
				Delta: &dto.ClaudeMediaMessage{
					StopReason: kitutil.GetPointer[string](stopReason),
				},
			})
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			state.Done = true
		}
		return claudeResponses
	} else {
		chosenChoice := openAIResponse.Choices[0]
		doneChunk := chosenChoice.FinishReason != nil && *chosenChoice.FinishReason != ""
		if doneChunk {
			state.FinishReason = *chosenChoice.FinishReason
		}

		var claudeResponse dto.ClaudeResponse
		var isEmpty bool
		claudeResponse.Type = "content_block_delta"
		if len(chosenChoice.Delta.ToolCalls) > 0 {
			toolCalls := chosenChoice.Delta.ToolCalls
			if state.LastMessagesType != convmeta.LastMessageTypeTools {
				stopOpenBlocksAndAdvance()
				state.ToolCallBaseIndex = state.Index
				state.ToolCallMaxIndexOffset = 0
			}
			state.LastMessagesType = convmeta.LastMessageTypeTools
			base := state.ToolCallBaseIndex
			maxOffset := state.ToolCallMaxIndexOffset

			for i, toolCall := range toolCalls {
				offset := 0
				if toolCall.Index != nil {
					offset = *toolCall.Index
				} else {
					offset = i
				}
				if offset > maxOffset {
					maxOffset = offset
				}
				blockIndex := base + offset

				idx := blockIndex
				if toolCall.Function.Name != "" {
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: &idx,
						Type:  "content_block_start",
						ContentBlock: &dto.ClaudeMediaMessage{
							Id:    toolCall.ID,
							Type:  "tool_use",
							Name:  toolCall.Function.Name,
							Input: map[string]interface{}{},
						},
					})
				}

				if len(toolCall.Function.Arguments) > 0 {
					claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
						Index: &idx,
						Type:  "content_block_delta",
						Delta: &dto.ClaudeMediaMessage{
							Type:        "input_json_delta",
							PartialJson: &toolCall.Function.Arguments,
						},
					})
				}
			}
			state.ToolCallMaxIndexOffset = maxOffset
			state.Index = base + maxOffset
		} else {
			reasoning := chosenChoice.Delta.GetReasoningContent()
			textContent := chosenChoice.Delta.GetContentString()
			if reasoning != "" || textContent != "" {
				if reasoning != "" {
					if state.LastMessagesType != convmeta.LastMessageTypeThinking {
						stopOpenBlocksAndAdvance()
						idx := state.Index
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: &idx,
							Type:  "content_block_start",
							ContentBlock: &dto.ClaudeMediaMessage{
								Type:     "thinking",
								Thinking: kitutil.GetPointer[string](""),
							},
						})
					}
					state.LastMessagesType = convmeta.LastMessageTypeThinking
					claudeResponse.Delta = &dto.ClaudeMediaMessage{
						Type:     "thinking_delta",
						Thinking: &reasoning,
					}
				} else {
					if state.LastMessagesType != convmeta.LastMessageTypeText {
						stopOpenBlocksAndAdvance()
						state.CitationBlockOffset = state.CitationTextLength
						idx := state.Index
						claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
							Index: &idx,
							Type:  "content_block_start",
							ContentBlock: &dto.ClaudeMediaMessage{
								Type: "text",
								Text: kitutil.GetPointer[string](""),
							},
						})
					}
					state.LastMessagesType = convmeta.LastMessageTypeText
					state.CitationTextLength += utf8.RuneCountInString(textContent)
					claudeResponse.Delta = &dto.ClaudeMediaMessage{
						Type: "text_delta",
						Text: kitutil.GetPointer[string](textContent),
					}
				}
			} else {
				isEmpty = true
			}
		}

		claudeResponse.Index = kitutil.GetPointer[int](state.Index)
		if !isEmpty && claudeResponse.Delta != nil {
			claudeResponses = append(claudeResponses, &claudeResponse)
		}

		citationEvents, citationErr := appendClaudeStreamCitations(state, chosenChoice.Delta.Annotations)
		if citationErr != nil {
			state.ResponseError = citationErr
			return nil
		}
		claudeResponses = append(claudeResponses, citationEvents...)
		if doneChunk || state.Done {
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = state.Usage
			}
			// Content and finish_reason may share a chunk. Emit content now,
			// but leave blocks open until the terminal usage is available.
			if oaiUsage == nil {
				return claudeResponses
			}
			appendStopOpenBlocks()
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type:  "message_delta",
					Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
					Delta: &dto.ClaudeMediaMessage{
						StopReason: kitutil.GetPointer[string](stopReasonOpenAI2Claude(state.FinishReason)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			state.Done = true
			return claudeResponses
		}
	}

	return claudeResponses
}

func FinalizeStreamResponseOpenAI2Claude(info convmeta.Meta) []*dto.ClaudeResponse {
	if info == nil {
		info = &convmeta.Values{}
	}
	state := info.EnsureClaudeConvertInfo()
	if state.Done {
		return nil
	}

	stopReason := stopReasonOpenAI2Claude(state.FinishReason)
	if stopReason == "" {
		stopReason = "end_turn"
	}
	responses := stopOpenBlocks(state)
	responses = append(responses,
		&dto.ClaudeResponse{
			Type:  "message_delta",
			Usage: buildClaudeUsageFromOpenAIUsage(state.Usage),
			Delta: &dto.ClaudeMediaMessage{
				StopReason: kitutil.GetPointer[string](stopReason),
			},
		},
		&dto.ClaudeResponse{Type: "message_stop"},
	)
	state.Done = true
	return responses
}

func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info convmeta.Meta) *dto.ClaudeResponse {
	var stopReason string
	contents := make([]dto.ClaudeMediaMessage, 0)
	claudeResponse := &dto.ClaudeResponse{
		Id:    openAIResponse.Id,
		Type:  "message",
		Role:  "assistant",
		Model: openAIResponse.Model,
	}
	for _, choice := range openAIResponse.Choices {
		stopReason = stopReasonOpenAI2Claude(choice.FinishReason)
		textContent := choice.Message.StringContent()
		toolCalls := choice.Message.ParseToolCalls()
		if textContent != "" || len(toolCalls) == 0 {
			blocks, err := CitationTextBlocks(textContent, choice.Message.Annotations)
			if err != nil {
				return nil
			}
			contents = append(contents, blocks...)
		}
		for _, toolUse := range toolCalls {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "tool_use"
			claudeContent.Id = toolUse.ID
			claudeContent.Name = toolUse.Function.Name
			mapParams := map[string]interface{}{}
			if strings.TrimSpace(toolUse.Function.Arguments) != "" {
				var parsed map[string]interface{}
				if err := kitutil.Unmarshal([]byte(toolUse.Function.Arguments), &parsed); err == nil && parsed != nil {
					mapParams = parsed
				}
			}
			claudeContent.Input = mapParams
			contents = append(contents, claudeContent)
		}
	}
	claudeResponse.Content = contents
	claudeResponse.StopReason = stopReason
	claudeResponse.Usage = buildClaudeUsageFromOpenAIUsage(&openAIResponse.Usage)

	return claudeResponse
}

func stopReasonOpenAI2Claude(reason string) string {
	return reasonmap.OpenAIFinishReasonToClaudeStopReason(reason)
}
