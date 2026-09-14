package relayconvert

import (
	"context"

	"github.com/QuantumNous/new-api/relaykit/dto"
	claudemessages "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/claude_messages"
	geminichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/gemini_chat"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	oairesponses "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_responses"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ToolIdentityRequirements contains no call identifiers, arguments or results.
// The same facts protect planning and adapters whose schema correlates by name.
type ToolIdentityRequirements struct {
	ExplicitIDs    bool `json:"explicit_ids"`
	InvalidHistory bool `json:"invalid_history"`
	RequiresIDs    bool `json:"requires_ids"`
}

func (r *ToolIdentityRequirements) CheckNameOnly(from types.RelayFormat, policy types.ConversionLossPolicy) ([]types.ConversionDiagnostic, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	diagnostic := types.ConversionDiagnostic{From: from, To: types.RelayFormatGemini, Path: "tool_history"}
	switch {
	case r.InvalidHistory:
		diagnostic.Code, diagnostic.Severity = "invalid_tool_history", types.ConversionDiagnosticError
		diagnostic.Message = "tool results do not match a valid outstanding call history"
	case r.RequiresIDs:
		diagnostic.Code, diagnostic.Severity = "tool_identity_required", types.ConversionDiagnosticError
		diagnostic.Message = "this tool history becomes ambiguous when the upstream schema removes call identifiers"
	case r.ExplicitIDs:
		diagnostic.Code, diagnostic.Severity = "tool_ids_projected_to_names", types.ConversionDiagnosticWarning
		diagnostic.Message = "the upstream schema omits call identifiers; tool results use validated unambiguous names"
	default:
		return nil, nil
	}
	diagnostics := []types.ConversionDiagnostic{diagnostic}
	if r.InvalidHistory || r.RequiresIDs {
		return diagnostics, &types.ConversionLossError{Diagnostics: diagnostics}
	}
	return diagnostics, types.RejectConversionLoss(policy, diagnostics)
}

// ProjectGeminiToolHistoryWithoutIDs validates the original and projected
// histories with the existing Gemini resolver before modifying a copy.
func ProjectGeminiToolHistoryWithoutIDs(request *dto.GeminiChatRequest, policy types.ConversionLossPolicy) (*RequestResult, error) {
	requirements := analyzeGeminiToolIdentity(request)
	diagnostics, err := requirements.CheckNameOnly(types.RelayFormatGemini, policy)
	result := &RequestResult{From: types.RelayFormatGemini, To: types.RelayFormatGemini, Diagnostics: diagnostics}
	if err != nil {
		return result, err
	}
	data, err := kitutil.Marshal(request)
	if err != nil {
		return result, err
	}
	var projected dto.GeminiChatRequest
	if err := kitutil.Unmarshal(data, &projected); err != nil {
		return result, err
	}
	removeGeminiToolIDs(&projected)
	result.Value = &projected
	return result, nil
}

func analyzeToolIdentity(request any) *ToolIdentityRequirements {
	var chat *dto.GeneralOpenAIRequest
	var err error
	switch request := request.(type) {
	case dto.GeminiChatRequest:
		return analyzeToolIdentity(&request)
	case dto.GeneralOpenAIRequest:
		return analyzeToolIdentity(&request)
	case dto.ClaudeRequest:
		return analyzeToolIdentity(&request)
	case dto.OpenAIResponsesRequest:
		return analyzeToolIdentity(&request)
	case *dto.GeminiChatRequest:
		return analyzeGeminiToolIdentity(request)
	case *dto.GeneralOpenAIRequest:
		chat = request
	case *dto.ClaudeRequest:
		if request == nil {
			return &ToolIdentityRequirements{InvalidHistory: true}
		}
		history := &dto.ClaudeRequest{}
		for _, message := range request.Messages {
			if message.IsStringContent() || message.Content == nil {
				continue
			}
			blocks, parseErr := message.ParseContent()
			if parseErr != nil {
				return &ToolIdentityRequirements{InvalidHistory: true}
			}
			tools := make([]dto.ClaudeMediaMessage, 0)
			for _, block := range blocks {
				if block.Type == "tool_use" || block.Type == "tool_result" {
					tools = append(tools, block)
				}
			}
			if len(tools) > 0 {
				history.Messages = append(history.Messages, dto.ClaudeMessage{Role: message.Role, Content: tools})
			}
		}
		chat, err = claudemessages.ClaudeMessagesRequestToOpenAIChat(*history, nil)
	case *dto.OpenAIResponsesRequest:
		if request == nil {
			return &ToolIdentityRequirements{InvalidHistory: true}
		}
		var input []map[string]any
		if len(request.Input) == 0 || kitutil.Unmarshal(request.Input, &input) != nil {
			return nil
		}
		tools := make([]map[string]any, 0)
		for _, item := range input {
			switch item["type"] {
			case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
				tools = append(tools, item)
			}
		}
		if len(tools) == 0 {
			return nil
		}
		data, marshalErr := kitutil.Marshal(tools)
		if marshalErr != nil {
			return &ToolIdentityRequirements{InvalidHistory: true}
		}
		chat, err = oairesponses.ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{Model: "tool-history", Input: data})
	default:
		return nil
	}
	if err != nil || chat == nil {
		return &ToolIdentityRequirements{InvalidHistory: true}
	}
	// Keep only tool fields so analysis cannot resolve media or forward text.
	history := dto.GeneralOpenAIRequest{}
	for _, message := range chat.Messages {
		if len(message.ToolCalls) == 0 && message.Role != "tool" {
			continue
		}
		history.Messages = append(history.Messages, dto.Message{
			Role: message.Role, Name: message.Name, ToolCalls: message.ToolCalls,
			ToolCallId: message.ToolCallId, Content: "{}",
		})
	}
	if len(history.Messages) == 0 {
		return nil
	}
	gemini, err := oaichat.OpenAIChatRequestToGeminiGenerateContent(context.Background(), history, nil)
	if err != nil {
		return &ToolIdentityRequirements{InvalidHistory: true}
	}
	return analyzeGeminiToolIdentity(gemini)
}

func analyzeGeminiToolIdentity(request *dto.GeminiChatRequest) *ToolIdentityRequirements {
	requirements := &ToolIdentityRequirements{}
	if request == nil {
		requirements.InvalidHistory = true
		return requirements
	}
	// Reuse the established matching algorithm with only tool parts present.
	history := &dto.GeminiChatRequest{}
	for _, content := range request.Contents {
		tools := dto.GeminiChatContent{Role: content.Role}
		for _, part := range content.Parts {
			if part.FunctionCall == nil && part.FunctionResponse == nil {
				continue
			}
			if part.FunctionCall != nil {
				requirements.ExplicitIDs = requirements.ExplicitIDs || part.FunctionCall.ID != ""
			}
			if part.FunctionResponse != nil {
				var id string
				if kitutil.Unmarshal(part.FunctionResponse.ID, &id) == nil {
					requirements.ExplicitIDs = requirements.ExplicitIDs || id != ""
				}
			}
			tools.Parts = append(tools.Parts, part)
		}
		if len(tools.Parts) > 0 {
			history.Contents = append(history.Contents, tools)
		}
	}
	if _, err := geminichat.GeminiGenerateContentRequestToOpenAIChat(history, nil); err != nil {
		requirements.InvalidHistory = true
	}
	data, err := kitutil.Marshal(history)
	var projected dto.GeminiChatRequest
	if err != nil || kitutil.Unmarshal(data, &projected) != nil {
		requirements.InvalidHistory = true
		return requirements
	}
	removeGeminiToolIDs(&projected)
	if _, err := geminichat.GeminiGenerateContentRequestToOpenAIChat(&projected, nil); err != nil {
		requirements.RequiresIDs = true
	}
	for i := range request.Requests {
		child := analyzeGeminiToolIdentity(&request.Requests[i])
		if child == nil {
			continue
		}
		requirements.ExplicitIDs = requirements.ExplicitIDs || child.ExplicitIDs
		requirements.InvalidHistory = requirements.InvalidHistory || child.InvalidHistory
		requirements.RequiresIDs = requirements.RequiresIDs || child.RequiresIDs
	}
	if len(history.Contents) == 0 && len(request.Requests) == 0 {
		return nil
	}
	return requirements
}

func removeGeminiToolIDs(request *dto.GeminiChatRequest) {
	for i := range request.Contents {
		for j := range request.Contents[i].Parts {
			part := &request.Contents[i].Parts[j]
			if part.FunctionCall != nil {
				part.FunctionCall.ID = ""
			}
			if part.FunctionResponse != nil {
				part.FunctionResponse.ID = nil
			}
		}
	}
	for i := range request.Requests {
		removeGeminiToolIDs(&request.Requests[i])
	}
}
