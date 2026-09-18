package relayconvert

import (
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type responseArtifactStreamState struct {
	claude      map[int]dto.ClaudeMediaMessage
	arguments   map[int]string
	responses   map[string]dto.ResponsesOutput
	order       []string
	seen        map[string]string
	diagnostics []types.ConversionDiagnostic
}

func (s *ResponseStreamState) convertHostedStreamArtifacts(response any, info convmeta.Meta) ([]any, error) {
	if s.From == s.To {
		return nil, nil
	}
	if s.artifacts == nil {
		s.artifacts = &responseArtifactStreamState{claude: make(map[int]dto.ClaudeMediaMessage), arguments: make(map[int]string), responses: make(map[string]dto.ResponsesOutput), seen: make(map[string]string)}
	}
	artifacts := s.artifacts
	changed := false
	switch r := response.(type) {
	case dto.ClaudeResponse:
		return s.convertHostedStreamArtifacts(&r, info)
	case dto.ResponsesStreamResponse:
		return s.convertHostedStreamArtifacts(&r, info)
	case dto.GeminiChatResponse:
		return s.convertHostedStreamArtifacts(&r, info)
	case *dto.ClaudeResponse:
		if r.Index == nil {
			break
		}
		index := *r.Index
		if r.Type == "content_block_start" && r.ContentBlock != nil && isClaudeHostedResponse(r.ContentBlock.Type) {
			artifacts.claude[index] = *r.ContentBlock
			changed = true
		}
		if r.Type == "content_block_delta" && r.Delta != nil && r.Delta.PartialJson != nil {
			if _, found := artifacts.claude[index]; found {
				artifacts.arguments[index] += *r.Delta.PartialJson
			}
		}
		if r.Type == "content_block_stop" {
			block, found := artifacts.claude[index]
			if !found {
				break
			}
			if args := artifacts.arguments[index]; args != "" {
				if err := kitutil.Unmarshal([]byte(args), &block.Input); err != nil {
					return nil, fmt.Errorf("invalid hosted tool arguments: %w", err)
				}
				artifacts.claude[index] = block
			}
			changed = true
		}
	case *dto.ResponsesStreamResponse:
		if r.Type == "response.output_item.done" && r.Item != nil && isResponsesHostedResponse(r.Item.Type) {
			if _, found := artifacts.responses[r.Item.ID]; !found {
				artifacts.order = append(artifacts.order, r.Item.ID)
			}
			artifacts.responses[r.Item.ID] = *r.Item
			changed = true
		}
		if r.Response != nil {
			for _, item := range r.Response.Output {
				if isResponsesHostedResponse(item.Type) {
					if _, found := artifacts.responses[item.ID]; !found {
						artifacts.order = append(artifacts.order, item.ID)
					}
					artifacts.responses[item.ID] = item
					changed = true
				}
			}
		}
	case *dto.GeminiChatResponse:
		_, set := extractResponseArtifacts(s.From, r)
		if len(set.diagnostics) > 0 {
			artifacts.diagnostics = set.diagnostics
			changed = true
		}
		for _, item := range set.responses {
			if _, found := artifacts.responses[item.item.ID]; !found {
				artifacts.order = append(artifacts.order, item.item.ID)
			}
			artifacts.responses[item.item.ID] = item.item
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	set := responseArtifacts{from: s.From, diagnostics: artifacts.diagnostics}
	indexes := make([]int, 0, len(artifacts.claude))
	for index := range artifacts.claude {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		set.claude = append(set.claude, positionedClaudeArtifact{index, artifacts.claude[index]})
	}
	for i, id := range artifacts.order {
		set.responses = append(set.responses, positionedResponsesArtifact{i, artifacts.responses[id]})
	}
	var target any
	switch s.To {
	case types.RelayFormatOpenAIResponses:
		target = &dto.OpenAIResponsesResponse{}
	case types.RelayFormatClaude:
		target = &dto.ClaudeResponse{}
	default:
		target = &dto.OpenAITextResponse{}
	}
	converted, diagnostics, err := attachResponseArtifacts(s.To, target, set, info)
	s.rememberDiagnostics(diagnostics)
	if err != nil {
		return nil, err
	}
	var output []any
	switch value := converted.(type) {
	case *dto.OpenAIResponsesResponse:
		for _, step := range s.stepStates {
			state, ok := step.(*ChatToResponsesStreamState)
			if !ok {
				continue
			}
			for _, item := range value.Output {
				raw, err := kitutil.Marshal(item)
				if err != nil {
					return nil, err
				}
				if artifacts.seen[item.ID] == string(raw) {
					continue
				}
				artifacts.seen[item.ID] = string(raw)
				for _, event := range state.UpsertHostedOutput(item) {
					output = append(output, event)
				}
			}
		}
	case *dto.ClaudeResponse:
		for _, block := range value.Content {
			key := block.Id + ":" + block.Type + ":" + block.ToolUseId
			raw, err := kitutil.Marshal(block)
			if err != nil {
				return nil, err
			}
			if artifacts.seen[key] == string(raw) {
				continue
			}
			if _, exists := artifacts.seen[key]; exists {
				continue
			} // terminal item snapshots cannot append a second call
			artifacts.seen[key] = string(raw)
			for _, event := range oaichat.AppendHostedClaudeBlock(info, block) {
				output = append(output, event)
			}
		}
	}
	return output, nil
}

func (s *ResponseStreamState) rememberDiagnostics(diagnostics []types.ConversionDiagnostic) {
	for _, diagnostic := range diagnostics {
		found := false
		for _, existing := range s.diagnostics {
			if existing.Code == diagnostic.Code && existing.Path == diagnostic.Path && existing.From == diagnostic.From && existing.To == diagnostic.To {
				found = true
				break
			}
		}
		if !found {
			s.diagnostics = append(s.diagnostics, diagnostic)
			s.pendingDiagnostics = append(s.pendingDiagnostics, diagnostic)
		}
	}
}
