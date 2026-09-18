package oaichat

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func ChatAnnotationsToGemini(raw json.RawMessage, text string) (*dto.GeminiGroundingMetadata, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var annotations []map[string]any
	if err := kitutil.Unmarshal(raw, &annotations); err != nil {
		return nil, err
	}
	var chunks, supports []any
	runes := []rune(text)
	for _, annotation := range annotations {
		citation, ok := annotation["url_citation"].(map[string]any)
		if !ok {
			citation = annotation
		}
		start, startOK := annotationIndex(citation["start_index"])
		end, endOK := annotationIndex(citation["end_index"])
		if !startOK || !endOK || start < 0 || end <= start || end > len(runes) {
			return nil, fmt.Errorf("citation_range_unrepresentable: Gemini grounding requires an exact text range")
		}
		if _, ok := citation["url"].(string); !ok {
			return nil, fmt.Errorf("citation_source_unrepresentable")
		}
		index := len(chunks)
		chunks = append(chunks, map[string]any{"web": map[string]any{"uri": citation["url"], "title": citation["title"]}})
		supports = append(supports, map[string]any{"segment": map[string]any{"partIndex": 0, "startIndex": len(string(runes[:start])), "endIndex": len(string(runes[:end])), "text": string(runes[start:end])}, "groundingChunkIndices": []int{index}})
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	chunkJSON, _ := kitutil.Marshal(chunks)
	supportJSON, _ := kitutil.Marshal(supports)
	return &dto.GeminiGroundingMetadata{GroundingChunks: chunkJSON, GroundingSupports: supportJSON}, nil
}

func chatAnnotationsToResponses(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 {
		return []any{}, nil
	}
	var annotations []map[string]any
	if err := kitutil.Unmarshal(raw, &annotations); err != nil {
		return nil, fmt.Errorf("invalid response annotations: %w", err)
	}
	result := make([]any, 0, len(annotations))
	for _, annotation := range annotations {
		if citation, ok := annotation["url_citation"].(map[string]any); ok {
			flattened := make(map[string]any, len(citation)+1)
			for k, v := range citation {
				flattened[k] = v
			}
			flattened["type"] = "url_citation"
			result = append(result, flattened)
		} else {
			result = append(result, annotation)
		}
	}
	return result, nil
}

func (s *ChatToResponsesStreamState) appendAnnotations(raw json.RawMessage) ([]ChatToResponsesStreamEvent, error) {
	annotations, err := chatAnnotationsToResponses(raw)
	if err != nil || len(annotations) == 0 {
		return nil, err
	}
	if s.seenAnnotations == nil {
		s.seenAnnotations = make(map[string]struct{})
	}
	var events []ChatToResponsesStreamEvent
	for _, annotation := range annotations {
		if value, ok := annotation.(map[string]any); ok && value["type"] == "url_citation" && s.textRuneOffset > 0 {
			for _, key := range []string{"start_index", "end_index"} {
				index, ok := annotationIndex(value[key])
				if !ok || index < s.textRuneOffset {
					return nil, fmt.Errorf("citation_range_unrepresentable: annotation references a closed response item")
				}
				value[key] = index - s.textRuneOffset
			}
		}
		encoded, err := kitutil.Marshal(annotation)
		if err != nil {
			return nil, err
		}
		if _, exists := s.seenAnnotations[string(encoded)]; exists {
			continue
		}
		s.seenAnnotations[string(encoded)] = struct{}{}
		index := len(s.annotations)
		s.annotations = append(s.annotations, annotation)
		if s.textStarted {
			events = append(events, responsesStreamEvent("response.output_text.annotation.added", dto.ResponsesStreamResponse{ItemID: s.messageID(), OutputIndex: intPtr(s.textOutputIndex), ContentIndex: intPtr(0), AnnotationIndex: intPtr(index), Annotation: encoded}))
		}
	}
	return events, nil
}
