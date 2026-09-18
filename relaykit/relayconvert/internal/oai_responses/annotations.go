package oairesponses

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func responsesAnnotationsToChat(response *dto.OpenAIResponsesResponse) json.RawMessage {
	var annotations []any
	offset := 0
	for _, output := range response.Output {
		if output.Type != "message" {
			continue
		}
		for _, part := range output.Content {
			for _, annotation := range part.Annotations {
				encoded, err := kitutil.Marshal(annotation)
				if err != nil {
					continue
				}
				var value map[string]any
				if kitutil.Unmarshal(encoded, &value) != nil {
					continue
				}
				if value["type"] == "url_citation" {
					delete(value, "type")
					for _, key := range []string{"start_index", "end_index"} {
						if n, ok := value[key].(float64); ok {
							value[key] = n + float64(offset)
						}
					}
					annotations = append(annotations, map[string]any{"type": "url_citation", "url_citation": value})
				} else {
					annotations = append(annotations, value)
				}
			}
			offset += utf8.RuneCountInString(part.Text)
		}
	}
	if len(annotations) == 0 {
		return nil
	}
	encoded, _ := kitutil.Marshal(annotations)
	return encoded
}

func (s *ResponsesToChatStreamState) annotationChunks(response *dto.OpenAIResponsesResponse) []dto.ChatCompletionsStreamResponse {
	raw := responsesAnnotationsToChat(response)
	if len(raw) == 0 {
		return nil
	}
	var annotations []any
	if kitutil.Unmarshal(raw, &annotations) != nil {
		return nil
	}
	if s.seenAnnotations == nil {
		s.seenAnnotations = make(map[string]struct{})
	}
	var fresh []any
	for _, annotation := range annotations {
		key, err := kitutil.Marshal(annotation)
		if err != nil {
			continue
		}
		if _, exists := s.seenAnnotations[string(key)]; exists {
			continue
		}
		s.seenAnnotations[string(key)] = struct{}{}
		fresh = append(fresh, annotation)
	}
	if len(fresh) == 0 {
		return nil
	}
	encoded, _ := kitutil.Marshal(fresh)
	return []dto.ChatCompletionsStreamResponse{s.makeChunk(dto.ChatCompletionsStreamResponseChoiceDelta{Annotations: encoded}, nil)}
}

func validateResponsesTerminal(response *dto.OpenAIResponsesResponse) error {
	if response == nil {
		return nil
	}
	status := responseStatusString(response)
	if status == "incomplete" {
		if response.IncompleteDetails == nil || (response.IncompleteDetails.Reason != "max_output_tokens" && response.IncompleteDetails.Reason != "content_filter") {
			return fmt.Errorf("response_termination_unsupported: cannot represent incomplete reason")
		}
	}
	if status == "failed" || status == "cancelled" {
		return fmt.Errorf("response_termination_unsupported: cannot represent %s as Chat completion", status)
	}
	return nil
}
