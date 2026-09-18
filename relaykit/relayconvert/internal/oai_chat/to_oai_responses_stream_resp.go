package oaichat

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

type ChatToResponsesStreamEvent struct {
	Type    string
	Payload dto.ResponsesStreamResponse
}

type ChatToResponsesStreamState struct {
	completedMessages map[int]dto.ResponsesOutput
	previousText      strings.Builder
	textRuneOffset    int
	messageSerial     int
	hosted            map[string]dto.ResponsesOutput
	hostedIndexes     map[string]int
	annotations       []any
	seenAnnotations   map[string]struct{}
	ID                string
	Model             string
	Created           int64
	Usage             *dto.Usage

	status            string
	incompleteDetails *dto.IncompleteDetails
	sentCreated       bool
	textOutputIndex   int
	textStarted       bool
	textDone          bool
	reasoningIndex    int
	reasoningStarted  bool
	reasoningDone     bool
	finalized         bool
	nextOutputIndex   int
	toolsByIndex      map[int]*chatToResponsesStreamTool
	outputOrder       []chatToResponsesOutputRef
	text              strings.Builder
	reasoning         strings.Builder
	nextSequence      int
}

type chatToResponsesStreamTool struct {
	ChatIndex   int
	OutputIndex int
	ID          string
	Name        string
	Arguments   strings.Builder
	Done        bool
}

type chatToResponsesOutputRef struct {
	Kind      string
	ToolIndex int
}

func NewChatToResponsesStreamState(id string, model string) *ChatToResponsesStreamState {
	return &ChatToResponsesStreamState{
		annotations:     []any{},
		ID:              id,
		Model:           model,
		Created:         time.Now().Unix(),
		Usage:           &dto.Usage{},
		status:          "completed",
		textOutputIndex: -1,
		reasoningIndex:  -1,
		toolsByIndex:    make(map[int]*chatToResponsesStreamTool),
	}
}

func ChatCompletionsStreamChunkToResponsesEvents(chunk *dto.ChatCompletionsStreamResponse, state *ChatToResponsesStreamState) ([]ChatToResponsesStreamEvent, error) {
	if chunk == nil || state == nil {
		return nil, nil
	}
	if len(chunk.Choices) > 1 || len(chunk.Choices) == 1 && chunk.Choices[0].Index != 0 {
		return nil, fmt.Errorf("Responses stream conversion cannot represent multiple choices")
	}
	if state.finalized {
		return nil, fmt.Errorf("Responses stream is already finalized")
	}
	if state.ID == "" {
		state.ID = chunk.Id
	}
	if state.Model == "" {
		state.Model = chunk.Model
	}
	if state.Created == 0 {
		state.Created = chunk.Created
	}
	if chunk.Usage != nil {
		state.Usage = UsageFromChatUsage(chunk.Usage)
	}

	events := make([]ChatToResponsesStreamEvent, 0)
	if !state.sentCreated {
		state.sentCreated = true
		events = append(events, responsesStreamEvent(responsesEventCreated, dto.ResponsesStreamResponse{
			Type:     responsesEventCreated,
			Response: state.createdResponse(),
		}))
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.GetReasoningContent() != "" {
			events = append(events, state.appendReasoningDelta(choice.Delta.GetReasoningContent())...)
		}
		if choice.Delta.GetContentString() != "" {
			events = append(events, state.appendTextDelta(choice.Delta.GetContentString())...)
		}
		annotationEvents, err := state.appendAnnotations(choice.Delta.Annotations)
		if err != nil {
			return nil, err
		}
		events = append(events, annotationEvents...)
		for _, toolCall := range choice.Delta.ToolCalls {
			toolEvents, err := state.appendToolCallDelta(toolCall)
			if err != nil {
				return nil, err
			}
			events = append(events, toolEvents...)
		}
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			state.applyFinishReason(*choice.FinishReason)
			events = append(events, state.doneDeltaEvents()...)
		}
	}
	return state.sequence(events), nil
}

func FinalizeChatCompletionsStreamToResponses(state *ChatToResponsesStreamState) []ChatToResponsesStreamEvent {
	if state == nil || state.finalized {
		return nil
	}
	events := state.doneDeltaEvents()
	state.finalized = true
	resp := state.finalResponse()
	eventType := responsesEventCompleted
	if state.status == "incomplete" {
		eventType = responsesEventIncomplete
	}
	events = append(events, responsesStreamEvent(eventType, dto.ResponsesStreamResponse{
		Type:     eventType,
		Response: resp,
	}))
	return state.sequence(events)
}

func (s *ChatToResponsesStreamState) sequence(events []ChatToResponsesStreamEvent) []ChatToResponsesStreamEvent {
	for i := range events {
		sequence := s.nextSequence
		s.nextSequence++
		events[i].Payload.SequenceNumber = &sequence
	}
	return events
}

func (s *ChatToResponsesStreamState) EndAbnormally(outcome string) []ChatToResponsesStreamEvent {
	if s == nil || s.finalized {
		return nil
	}
	s.status = outcome
	s.finalized = true
	events := s.doneDeltaEvents()
	response := s.finalResponse()
	if outcome == "failed" {
		response.Error = map[string]any{"code": "upstream_stream_error", "message": "upstream stream ended unexpectedly"}
	}
	events = append(events, responsesStreamEvent("response."+outcome, dto.ResponsesStreamResponse{Type: "response." + outcome, Response: response}))
	return s.sequence(events)
}

func (s *ChatToResponsesStreamState) UsageText() string {
	if s == nil {
		return ""
	}
	return s.previousText.String() + s.text.String()
}

func (s *ChatToResponsesStreamState) appendTextDelta(delta string) []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if !s.textStarted {
		s.textStarted = true
		s.textOutputIndex = s.nextIndex("message", s.nextOutputIndex)
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(s.textOutputIndex),
			Item: &dto.ResponsesOutput{
				Type:    responsesOutputTypeMessage,
				ID:      s.messageID(),
				Status:  "in_progress",
				Role:    "assistant",
				Content: []dto.ResponsesOutputContent{},
			},
		}))
		events = append(events, responsesStreamEvent("response.content_part.added", dto.ResponsesStreamResponse{
			Type: "response.content_part.added", OutputIndex: intPtr(s.textOutputIndex), ContentIndex: intPtr(0), ItemID: s.messageID(),
			Part: outputTextPart(""),
		}))
	}
	s.text.WriteString(delta)
	events = append(events, responsesStreamEvent(responsesEventOutputTextDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventOutputTextDelta,
		OutputIndex:  intPtr(s.textOutputIndex),
		ContentIndex: intPtr(0),
		Delta:        delta,
		ItemID:       s.messageID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) appendReasoningDelta(delta string) []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if !s.reasoningStarted {
		s.reasoningStarted = true
		s.reasoningIndex = s.nextIndex("reasoning", -1)
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(s.reasoningIndex),
			Item: &dto.ResponsesOutput{
				Type:    responsesOutputTypeReasoning,
				ID:      s.reasoningID(),
				Status:  "in_progress",
				Summary: []dto.ResponsesReasoningSummaryPart{},
			},
		}))
		events = append(events, responsesStreamEvent("response.reasoning_summary_part.added", dto.ResponsesStreamResponse{
			Type: "response.reasoning_summary_part.added", OutputIndex: intPtr(s.reasoningIndex), SummaryIndex: intPtr(0), ItemID: s.reasoningID(),
			Part: &dto.ResponsesReasoningSummaryPart{Type: "summary_text", Text: ""},
		}))
	}
	s.reasoning.WriteString(delta)
	events = append(events, responsesStreamEvent(responsesEventReasoningSummaryDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventReasoningSummaryDelta,
		OutputIndex:  intPtr(s.reasoningIndex),
		SummaryIndex: intPtr(0),
		Delta:        delta,
		ItemID:       s.reasoningID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) appendToolCallDelta(toolCall dto.ToolCallResponse) ([]ChatToResponsesStreamEvent, error) {
	chatIndex := 0
	if toolCall.Index != nil {
		chatIndex = *toolCall.Index
	}
	tool := s.toolsByIndex[chatIndex]
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if tool == nil {
		tool = &chatToResponsesStreamTool{
			ChatIndex:   chatIndex,
			OutputIndex: s.nextIndex("tool", chatIndex),
			ID:          strings.TrimSpace(toolCall.ID),
			Name:        strings.TrimSpace(toolCall.Function.Name),
		}
		if tool.ID == "" {
			tool.ID = fmt.Sprintf("%s_call_%d", s.ID, chatIndex)
		}
		s.toolsByIndex[chatIndex] = tool
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(tool.OutputIndex),
			ItemID:      tool.ID,
			Item: &dto.ResponsesOutput{
				Type:      responsesOutputTypeFunctionCall,
				ID:        tool.ID,
				Status:    "in_progress",
				CallId:    tool.ID,
				Name:      tool.Name,
				Arguments: []byte(`""`),
			},
		}))
	}
	if strings.TrimSpace(toolCall.ID) != "" {
		tool.ID = strings.TrimSpace(toolCall.ID)
	}
	if strings.TrimSpace(toolCall.Function.Name) != "" {
		tool.Name = strings.TrimSpace(toolCall.Function.Name)
	}
	if toolCall.Function.Arguments != "" {
		tool.Arguments.WriteString(toolCall.Function.Arguments)
		events = append(events, responsesStreamEvent(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
			Type:        responsesEventFunctionArgsDelta,
			OutputIndex: intPtr(tool.OutputIndex),
			ItemID:      tool.ID,
			Delta:       toolCall.Function.Arguments,
		}))
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) doneDeltaEvents() []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0)
	status := s.outputStatus()
	if s.textStarted && !s.textDone {
		s.textDone = true
		events = append(events, responsesStreamEvent("response.output_text.done", dto.ResponsesStreamResponse{
			Type:         "response.output_text.done",
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(0),
			ItemID:       s.messageID(),
			Text:         stringPtr(s.text.String()),
		}))
		events = append(events, responsesStreamEvent("response.content_part.done", dto.ResponsesStreamResponse{
			Type: "response.content_part.done", OutputIndex: intPtr(s.textOutputIndex), ContentIndex: intPtr(0), ItemID: s.messageID(),
			Part: outputTextPart(s.text.String()),
		}))
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(s.textOutputIndex),
			Item:        s.messageOutput(status),
		}))
	}
	if s.reasoningStarted && !s.reasoningDone {
		s.reasoningDone = true
		events = append(events, responsesStreamEvent(responsesEventReasoningSummaryDone, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningSummaryDone,
			OutputIndex:  intPtr(s.reasoningIndex),
			SummaryIndex: intPtr(0),
			ItemID:       s.reasoningID(),
			Text:         stringPtr(s.reasoning.String()),
		}))
		events = append(events, responsesStreamEvent("response.reasoning_summary_part.done", dto.ResponsesStreamResponse{
			Type: "response.reasoning_summary_part.done", OutputIndex: intPtr(s.reasoningIndex), SummaryIndex: intPtr(0), ItemID: s.reasoningID(),
			Part: &dto.ResponsesReasoningSummaryPart{Type: "summary_text", Text: s.reasoning.String()},
		}))
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(s.reasoningIndex),
			Item:        s.reasoningOutput(status),
		}))
	}
	for _, tool := range s.sortedTools() {
		if tool.Done {
			continue
		}
		tool.Done = true
		events = append(events, responsesStreamEvent(responsesEventFunctionArgsDone, dto.ResponsesStreamResponse{
			Type:        responsesEventFunctionArgsDone,
			OutputIndex: intPtr(tool.OutputIndex),
			ItemID:      tool.ID,
			Arguments:   stringPtr(tool.Arguments.String()),
			Name:        tool.Name,
		}))
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(tool.OutputIndex),
			Item:        s.toolOutput(tool, status),
		}))
	}
	return events
}

func (s *ChatToResponsesStreamState) applyFinishReason(finishReason string) {
	if status, details := ResponsesStatusFromChatFinishReason(finishReason); status != "" {
		s.status = status
		s.incompleteDetails = details
	}
}

func (s *ChatToResponsesStreamState) finalResponse() *dto.OpenAIResponsesResponse {
	output := make([]dto.ResponsesOutput, 0, len(s.outputOrder))
	status := s.outputStatus()
	for _, ref := range s.outputOrder {
		switch ref.Kind {
		case "message":
			if completed, ok := s.completedMessages[ref.ToolIndex]; ok {
				output = append(output, completed)
			} else {
				output = append(output, *s.messageOutput(status))
			}
		case "reasoning":
			output = append(output, *s.reasoningOutput(status))
		case "tool":
			if tool := s.toolsByIndex[ref.ToolIndex]; tool != nil {
				output = append(output, *s.toolOutput(tool, status))
			}
		case "hosted":
			for id, index := range s.hostedIndexes {
				if index == ref.ToolIndex {
					output = append(output, s.hosted[id])
					break
				}
			}
		}
	}
	return &dto.OpenAIResponsesResponse{
		ID:                s.ID,
		Object:            "response",
		CreatedAt:         int(s.Created),
		Status:            []byte(fmt.Sprintf("%q", s.status)),
		IncompleteDetails: s.incompleteDetails,
		Model:             s.Model,
		Output:            output,
		Usage:             s.Usage,
	}
}

// UpsertHostedOutput shares the output index and sequence allocator with normal
// text/tool events, so a provider's later result updates the original call.
func (s *ChatToResponsesStreamState) UpsertHostedOutput(item dto.ResponsesOutput) []ChatToResponsesStreamEvent {
	if s == nil || s.finalized {
		return nil
	}
	if s.hosted == nil {
		s.hosted = make(map[string]dto.ResponsesOutput)
		s.hostedIndexes = make(map[string]int)
	}
	var events []ChatToResponsesStreamEvent
	if !s.sentCreated {
		s.sentCreated = true
		events = append(events, responsesStreamEvent(responsesEventCreated, dto.ResponsesStreamResponse{Response: s.createdResponse()}))
	}
	index, exists := s.hostedIndexes[item.ID]
	if !exists {
		if s.textStarted && !s.textDone {
			item := s.messageOutput("completed")
			events = append(events,
				responsesStreamEvent("response.output_text.done", dto.ResponsesStreamResponse{OutputIndex: intPtr(s.textOutputIndex), ContentIndex: intPtr(0), ItemID: s.messageID(), Text: stringPtr(s.text.String())}),
				responsesStreamEvent("response.content_part.done", dto.ResponsesStreamResponse{OutputIndex: intPtr(s.textOutputIndex), ContentIndex: intPtr(0), ItemID: s.messageID(), Part: outputTextPart(s.text.String())}),
				responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{OutputIndex: intPtr(s.textOutputIndex), Item: item}))
			if s.completedMessages == nil {
				s.completedMessages = make(map[int]dto.ResponsesOutput)
			}
			s.completedMessages[s.textOutputIndex] = *item
			s.previousText.WriteString(s.text.String())
			s.textRuneOffset += utf8.RuneCountInString(s.text.String())
			s.text.Reset()
			s.textStarted = false
			s.textOutputIndex = -1
			s.messageSerial++
			s.annotations = []any{}
			s.seenAnnotations = nil
		}
		index = s.nextOutputIndex
		s.nextIndex("hosted", index)
		s.hostedIndexes[item.ID] = index
		added := item
		added.Status = "in_progress"
		events = append(events, responsesStreamEvent(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{OutputIndex: intPtr(index), Item: &added}))
	}
	s.hosted[item.ID] = item
	if item.Status == "completed" || item.Status == "failed" {
		events = append(events, responsesStreamEvent(responsesEventOutputItemDone, dto.ResponsesStreamResponse{OutputIndex: intPtr(index), Item: &item}))
	}
	return s.sequence(events)
}

func (s *ChatToResponsesStreamState) createdResponse() *dto.OpenAIResponsesResponse {
	return &dto.OpenAIResponsesResponse{
		ID:        s.ID,
		Object:    "response",
		CreatedAt: int(s.Created),
		Status:    []byte(`"in_progress"`),
		Model:     s.Model,
		Output:    []dto.ResponsesOutput{},
	}
}

func (s *ChatToResponsesStreamState) nextIndex(kind string, toolIndex int) int {
	index := s.nextOutputIndex
	s.nextOutputIndex++
	s.outputOrder = append(s.outputOrder, chatToResponsesOutputRef{Kind: kind, ToolIndex: toolIndex})
	return index
}

func (s *ChatToResponsesStreamState) sortedTools() []*chatToResponsesStreamTool {
	indexes := make([]int, 0, len(s.toolsByIndex))
	for index := range s.toolsByIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	tools := make([]*chatToResponsesStreamTool, 0, len(indexes))
	for _, index := range indexes {
		tools = append(tools, s.toolsByIndex[index])
	}
	return tools
}

func (s *ChatToResponsesStreamState) outputStatus() string {
	if s.status == "incomplete" || s.status == "failed" {
		return "incomplete"
	}
	return "completed"
}

func (s *ChatToResponsesStreamState) messageID() string {
	return fmt.Sprintf("%s_msg_%d", s.ID, s.messageSerial)
}

func (s *ChatToResponsesStreamState) reasoningID() string {
	return fmt.Sprintf("%s_reasoning_0", s.ID)
}

func (s *ChatToResponsesStreamState) messageOutput(status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:   responsesOutputTypeMessage,
		ID:     s.messageID(),
		Status: status,
		Role:   "assistant",
		Content: []dto.ResponsesOutputContent{
			{
				Type:        "output_text",
				Text:        s.text.String(),
				Annotations: s.annotations,
			},
		},
	}
}

func (s *ChatToResponsesStreamState) reasoningOutput(status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:   responsesOutputTypeReasoning,
		ID:     s.reasoningID(),
		Status: status,
		Summary: []dto.ResponsesReasoningSummaryPart{
			{
				Type: "summary_text",
				Text: s.reasoning.String(),
			},
		},
	}
}

func (s *ChatToResponsesStreamState) toolOutput(tool *chatToResponsesStreamTool, status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:      responsesOutputTypeFunctionCall,
		ID:        tool.ID,
		Status:    status,
		CallId:    tool.ID,
		Name:      tool.Name,
		Arguments: chatArgumentsRawMessage(tool.Arguments.String()),
	}
}

func stringPtr(value string) *string { return &value }

func outputTextPart(text string) *dto.ResponsesReasoningSummaryPart {
	annotations := []any{}
	return &dto.ResponsesReasoningSummaryPart{Type: "output_text", Text: text, Annotations: &annotations}
}
