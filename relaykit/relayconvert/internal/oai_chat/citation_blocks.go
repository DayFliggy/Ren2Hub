package oaichat

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// CitationTextBlocks preserves URL annotation ranges by splitting text into
// Claude blocks, whose citations apply to the enclosing block rather than to
// Chat's absolute character indexes. Overlapping ranges share split blocks.
func CitationTextBlocks(text string, raw json.RawMessage) ([]dto.ClaudeMediaMessage, error) {
	if len(raw) == 0 {
		return []dto.ClaudeMediaMessage{{Type: "text", Text: &text}}, nil
	}
	var annotations []map[string]any
	if err := kitutil.Unmarshal(raw, &annotations); err != nil {
		return nil, err
	}
	runes := []rune(text)
	type rangedCitation struct {
		start, end int
		raw        json.RawMessage
	}
	ranges := make([]rangedCitation, 0, len(annotations))
	boundaries := map[int]struct{}{0: {}, len(runes): {}}
	for _, annotation := range annotations {
		citation, ok := annotation["url_citation"].(map[string]any)
		if !ok {
			citation = annotation
		}
		if !claudeCitationHasSource(citation) {
			return nil, fmt.Errorf("native_state_required: Claude web citations require original source quotation and encrypted reference")
		}
		start, startOK := annotationIndex(citation["start_index"])
		end, endOK := annotationIndex(citation["end_index"])
		if !startOK || !endOK || start < 0 || end <= start || end > len(runes) {
			return nil, fmt.Errorf("citation_range_unrepresentable: Claude conversion requires a valid output text range")
		}
		encoded, err := kitutil.Marshal([]any{annotation})
		if err != nil {
			return nil, err
		}
		converted := chatAnnotationsToClaude(encoded, text)
		if len(converted) != 1 {
			return nil, fmt.Errorf("citation_source_unrepresentable: Claude conversion requires a URL citation")
		}
		ranges = append(ranges, rangedCitation{start, end, converted[0]})
		boundaries[start], boundaries[end] = struct{}{}, struct{}{}
	}
	indexes := make([]int, 0, len(boundaries))
	for boundary := range boundaries {
		indexes = append(indexes, boundary)
	}
	sort.Ints(indexes)
	blocks := make([]dto.ClaudeMediaMessage, 0, len(indexes)-1)
	for i := 1; i < len(indexes); i++ {
		start, end := indexes[i-1], indexes[i]
		part := string(runes[start:end])
		block := dto.ClaudeMediaMessage{Type: "text", Text: &part}
		var citations []json.RawMessage
		for _, citation := range ranges {
			if citation.start <= start && citation.end >= end {
				citations = append(citations, citation.raw)
			}
		}
		if len(citations) > 0 {
			encoded, err := kitutil.Marshal(citations)
			if err != nil {
				return nil, err
			}
			block.Citations = encoded
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func appendClaudeStreamCitations(state *convmeta.ClaudeConvertInfo, raw json.RawMessage) ([]*dto.ClaudeResponse, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var annotations []map[string]any
	if err := kitutil.Unmarshal(raw, &annotations); err != nil {
		return nil, err
	}
	if state.CitationSeen == nil {
		state.CitationSeen = make(map[string]struct{})
	}
	var events []*dto.ClaudeResponse
	for _, annotation := range annotations {
		encoded, err := kitutil.Marshal(annotation)
		if err != nil {
			return nil, err
		}
		if _, exists := state.CitationSeen[string(encoded)]; exists {
			continue
		}
		citation, ok := annotation["url_citation"].(map[string]any)
		if !ok {
			citation = annotation
		}
		start, startOK := annotationIndex(citation["start_index"])
		end, endOK := annotationIndex(citation["end_index"])
		if !startOK || !endOK || start != state.CitationBlockOffset || end != state.CitationTextLength || state.LastMessagesType != convmeta.LastMessageTypeText {
			return nil, fmt.Errorf("citation_range_unrepresentable: streamed Claude citations must cover the current open text block")
		}
		if !claudeCitationHasSource(citation) {
			return nil, fmt.Errorf("native_state_required: Claude web citations require original source quotation and encrypted reference")
		}
		array, err := kitutil.Marshal([]any{annotation})
		if err != nil {
			return nil, err
		}
		converted := chatAnnotationsToClaude(array, "")
		if len(converted) != 1 {
			return nil, fmt.Errorf("citation_source_unrepresentable")
		}
		events = append(events, &dto.ClaudeResponse{Type: "content_block_delta", Index: kitutil.GetPointer(state.Index), Delta: &dto.ClaudeMediaMessage{Type: "citations_delta", Citation: converted[0]}})
		state.CitationSeen[string(encoded)] = struct{}{}
	}
	if len(events) > 0 {
		events = append(events, generateStopBlock(state.Index))
		state.Index++
		state.LastMessagesType = convmeta.LastMessageTypeNone
	}
	return events, nil
}
