package relayconvert

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Hosted results travel beside the Chat pivot. They must never be classified as
// client function calls or disappear when an intermediate format has no field.
// This boundary follows new-api's response-artifact design (7209b6db95ef).
type responseArtifacts struct {
	from             types.RelayFormat
	claude           []positionedClaudeArtifact
	responses        []positionedResponsesArtifact
	regularPositions []int
	diagnostics      []types.ConversionDiagnostic
	claudeRegular    []dto.ClaudeMediaMessage
	responsesRegular []dto.ResponsesOutput
}

type positionedClaudeArtifact struct {
	position int
	block    dto.ClaudeMediaMessage
}
type positionedResponsesArtifact struct {
	position int
	item     dto.ResponsesOutput
}

func responseArtifactDiagnostic(from, to types.RelayFormat, code, path string, semantic bool) types.ConversionDiagnostic {
	severity := types.ConversionDiagnosticWarning
	if semantic {
		severity = types.ConversionDiagnosticError
	}
	return types.ConversionDiagnostic{Code: code, Path: path, Message: "target response cannot represent this provider-specific information", From: from, To: to, Severity: severity}
}

func isClaudeHostedResponse(kind string) bool {
	return kind == "server_tool_use" || kind == "mcp_tool_use" || kind == "mcp_tool_result" || strings.HasSuffix(kind, "_tool_result") && kind != "tool_result"
}

func isResponsesHostedResponse(kind string) bool {
	switch kind {
	case "web_search_call", "file_search_call", "mcp_call", "mcp_list_tools", "mcp_approval_request", "code_interpreter_call", "image_generation_call":
		return true
	default:
		return false
	}
}

func extractResponseArtifacts(from types.RelayFormat, response any) (any, responseArtifacts) {
	set := responseArtifacts{from: from}
	switch r := response.(type) {
	case dto.ClaudeResponse:
		return extractResponseArtifacts(from, &r)
	case dto.OpenAIResponsesResponse:
		return extractResponseArtifacts(from, &r)
	case dto.GeminiChatResponse:
		return extractResponseArtifacts(from, &r)
	case *dto.ClaudeResponse:
		clone := *r
		clone.Content = make([]dto.ClaudeMediaMessage, 0, len(r.Content))
		for i, block := range r.Content {
			if isClaudeHostedResponse(block.Type) {
				set.claude = append(set.claude, positionedClaudeArtifact{i, block})
				continue
			}
			clone.Content = append(clone.Content, block)
			set.claudeRegular = append(set.claudeRegular, block)
			set.regularPositions = append(set.regularPositions, i)
		}
		return &clone, set
	case *dto.OpenAIResponsesResponse:
		clone := *r
		clone.Output = make([]dto.ResponsesOutput, 0, len(r.Output))
		for i, item := range r.Output {
			if isResponsesHostedResponse(item.Type) {
				set.responses = append(set.responses, positionedResponsesArtifact{i, item})
				continue
			}
			clone.Output = append(clone.Output, item)
			set.responsesRegular = append(set.responsesRegular, item)
			set.regularPositions = append(set.regularPositions, i)
		}
		return &clone, set
	case *dto.GeminiChatResponse:
		for i, candidate := range r.Candidates {
			if candidate.GroundingMetadata == nil || len(candidate.GroundingMetadata.WebSearchQueries) == 0 {
				continue
			}
			queries := candidate.GroundingMetadata.WebSearchQueries
			action, _ := kitutil.Marshal(map[string]any{"type": "search", "query": queries[0], "queries": queries})
			set.responses = append(set.responses, positionedResponsesArtifact{i + 1, dto.ResponsesOutput{Type: "web_search_call", ID: fmt.Sprintf("ws_%d", candidate.Index), Status: "completed", Action: action}})
		}
	}
	return response, set
}

func attachResponseArtifacts(to types.RelayFormat, response any, set responseArtifacts, info convmeta.Meta) (any, []types.ConversionDiagnostic, error) {
	if len(set.claude) == 0 && len(set.responses) == 0 {
		return response, nil, nil
	}
	diagnostics := append([]types.ConversionDiagnostic(nil), set.diagnostics...)
	switch target := response.(type) {
	case *dto.OpenAIResponsesResponse:
		items := append([]positionedResponsesArtifact(nil), set.responses...)
		byID := make(map[string]int)
		for _, artifact := range set.claude {
			block := artifact.block
			path := fmt.Sprintf("content[%d]", artifact.position)
			switch block.Type {
			case "server_tool_use":
				if block.Name != "web_search" {
					diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "hosted_tool_unrepresentable", path, true))
					continue
				}
				raw, _ := kitutil.Marshal(block.Input)
				var input map[string]any
				if err := kitutil.Unmarshal(raw, &input); err != nil {
					return nil, diagnostics, err
				}
				if input == nil {
					input = make(map[string]any)
				}
				input["type"] = "search"
				action, _ := kitutil.Marshal(input)
				byID[block.Id] = len(items)
				items = append(items, positionedResponsesArtifact{artifact.position, dto.ResponsesOutput{Type: "web_search_call", ID: block.Id, Status: "in_progress", Action: action}})
			case "mcp_tool_use":
				if block.ServerName == "" || block.Name == "" {
					diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "mcp_identity_missing", path, true))
					continue
				}
				arguments, _ := kitutil.Marshal(block.Input)
				arguments, _ = kitutil.Marshal(string(arguments))
				byID[block.Id] = len(items)
				items = append(items, positionedResponsesArtifact{artifact.position, dto.ResponsesOutput{Type: "mcp_call", ID: block.Id, Name: block.Name, ServerLabel: block.ServerName, Arguments: arguments, Status: "in_progress"}})
			case "web_search_tool_result", "mcp_tool_result":
				index, found := byID[block.ToolUseId]
				if !found {
					diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "hosted_tool_result_orphaned", path, true))
					continue
				}
				items[index].item.Status = "completed"
				if block.IsError != nil && *block.IsError {
					items[index].item.Status = "failed"
				}
				if block.Type == "mcp_tool_result" {
					text, ok := block.Content.(string)
					if !ok {
						raw, _ := kitutil.Marshal(block.Content)
						var content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						}
						if kitutil.Unmarshal(raw, &content) == nil && len(content) == 1 && content[0].Type == "text" {
							text, ok = content[0].Text, true
						}
					}
					if !ok {
						diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "mcp_result_unrepresentable", path, true))
						continue
					}
					encoded, _ := kitutil.Marshal(text)
					if items[index].item.Status == "failed" {
						items[index].item.ItemError = encoded
					} else {
						items[index].item.Output = encoded
					}
				} else if block.Content != nil {
					diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "web_search_result_omitted", path, false))
				}
			default:
				diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "hosted_tool_unrepresentable", path, true))
			}
		}
		if len(set.claudeRegular) > 0 {
			positioned := make(map[int][]dto.ResponsesOutput)
			for i, block := range set.claudeRegular {
				chat := ResponseClaude2OpenAI(&dto.ClaudeResponse{Content: []dto.ClaudeMediaMessage{block}, StopReason: "end_turn"})
				converted, _, err := ChatCompletionsResponseToResponsesResponse(chat, fmt.Sprintf("%s_part_%d", target.ID, set.regularPositions[i]))
				if err != nil {
					return nil, diagnostics, err
				}
				positioned[set.regularPositions[i]] = converted.Output
			}
			for _, item := range items {
				positioned[item.position] = append(positioned[item.position], item.item)
			}
			keys := make([]int, 0, len(positioned))
			for key := range positioned {
				keys = append(keys, key)
			}
			sort.Ints(keys)
			target.Output = nil
			for _, key := range keys {
				target.Output = append(target.Output, positioned[key]...)
			}
		} else {
			for _, item := range items {
				target.Output = append(target.Output, item.item)
			}
		}
	case *dto.ClaudeResponse:
		var blocks []dto.ClaudeMediaMessage
		positioned := make(map[int][]dto.ClaudeMediaMessage)
		for _, artifact := range set.responses {
			startBlock := len(blocks)
			item := artifact.item
			path := fmt.Sprintf("output[%d]", artifact.position)
			switch item.Type {
			case "web_search_call":
				var input map[string]any
				if err := kitutil.Unmarshal(item.Action, &input); err != nil {
					return nil, diagnostics, err
				}
				delete(input, "type")
				blocks = append(blocks, dto.ClaudeMediaMessage{Type: "server_tool_use", Id: item.ID, Name: "web_search", Input: input})
			case "mcp_call":
				if item.ServerLabel == "" || item.Name == "" {
					diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "mcp_identity_missing", path, true))
					continue
				}
				var input any
				if err := kitutil.Unmarshal([]byte(item.ArgumentsString()), &input); err != nil {
					return nil, diagnostics, err
				}
				blocks = append(blocks, dto.ClaudeMediaMessage{Type: "mcp_tool_use", Id: item.ID, Name: item.Name, ServerName: item.ServerLabel, Input: input})
				result := item.Output
				isError := len(item.ItemError) > 0 && string(item.ItemError) != "null"
				if isError {
					result = item.ItemError
				}
				if len(result) > 0 {
					var content any
					if err := kitutil.Unmarshal(result, &content); err != nil {
						return nil, diagnostics, err
					}
					blocks = append(blocks, dto.ClaudeMediaMessage{Type: "mcp_tool_result", ToolUseId: item.ID, Content: content, IsError: &isError})
				}
			default:
				diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "hosted_tool_unrepresentable", path, true))
			}
			positioned[artifact.position] = append(positioned[artifact.position], blocks[startBlock:]...)
		}
		if len(set.responsesRegular) > 0 {
			for i, item := range set.responsesRegular {
				source := &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{item}}
				chat, _, err := ResponsesResponseToChatCompletionsResponse(source, "")
				if err != nil {
					return nil, diagnostics, err
				}
				for _, choice := range chat.Choices {
					if _, err := oaichat.CitationTextBlocks(choice.Message.StringContent(), choice.Message.Annotations); err != nil {
						return nil, diagnostics, err
					}
				}
				converted := ResponseOpenAI2Claude(chat, info)
				positioned[set.regularPositions[i]] = converted.Content
			}
			keys := make([]int, 0, len(positioned))
			for key := range positioned {
				keys = append(keys, key)
			}
			sort.Ints(keys)
			target.Content = nil
			for _, key := range keys {
				target.Content = append(target.Content, positioned[key]...)
			}
		} else {
			target.Content = append(target.Content, blocks...)
		}
	default:
		diagnostics = append(diagnostics, responseArtifactDiagnostic(set.from, to, "hosted_tool_event_omitted", "response.hosted_tools", false))
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == types.ConversionDiagnosticError {
			return nil, diagnostics, &types.ConversionLossError{Diagnostics: []types.ConversionDiagnostic{diagnostic}}
		}
	}
	if err := types.RejectConversionLoss(convmeta.OptionsOf(info).EffectiveConversionPolicy(), diagnostics); err != nil {
		return nil, diagnostics, err
	}
	return response, diagnostics, nil
}

// A malformed annotation cannot be silently dropped in any conversion policy.
func validateResponseAnnotations(response any) error {
	var rawValues []json.RawMessage
	claude := false
	switch r := response.(type) {
	case dto.ClaudeResponse:
		return validateResponseAnnotations(&r)
	case dto.OpenAITextResponse:
		return validateResponseAnnotations(&r)
	case dto.OpenAIResponsesResponse:
		return validateResponseAnnotations(&r)
	case *dto.ClaudeResponse:
		claude = true
		for _, block := range r.Content {
			rawValues = append(rawValues, block.Citations)
		}
	case *dto.OpenAITextResponse:
		for _, choice := range r.Choices {
			rawValues = append(rawValues, choice.Message.Annotations)
		}
	case *dto.OpenAIResponsesResponse:
		for _, item := range r.Output {
			for _, content := range item.Content {
				if len(content.Annotations) > 0 {
					raw, err := kitutil.Marshal(content.Annotations)
					if err != nil {
						return err
					}
					rawValues = append(rawValues, raw)
				}
			}
		}
	}
	for _, raw := range rawValues {
		if len(raw) == 0 {
			continue
		}
		var values []map[string]any
		if err := kitutil.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("invalid response citations: %w", err)
		}
		if claude {
			for _, value := range values {
				if url, ok := value["url"].(string); !ok || url == "" {
					return fmt.Errorf("citation_source_unrepresentable: native document citation requires its original protocol")
				}
			}
		}
		if !claude {
			for _, value := range values {
				if value["type"] != "url_citation" {
					return fmt.Errorf("citation_source_unrepresentable: native citation requires its original protocol")
				}
			}
		}
	}
	return nil
}
