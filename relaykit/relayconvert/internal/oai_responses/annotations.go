package oairesponses

import (
	"encoding/json"
	"fmt"
	"sort"
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

// Annotation events can arrive independently of text and may be absent from a
// provider's final snapshot. Keep them by output/content index until terminal.
func (s *ResponsesToChatStreamState) rememberAnnotationEvent(event *dto.ResponsesStreamResponse) error {
	if s.annotationItems == nil {
		s.annotationItems = make(map[int]*dto.ResponsesOutput)
	}
	if event.OutputIndex == nil {
		return nil
	}
	index := *event.OutputIndex
	item := s.annotationItems[index]
	if item == nil {
		item = &dto.ResponsesOutput{Type: "message", ID: event.ItemID}
		s.annotationItems[index] = item
	}
	if event.Item != nil && event.Item.Type == "message" {
		copy := *event.Item
		copy.Content = append([]dto.ResponsesOutputContent(nil), event.Item.Content...)
		for i, part := range item.Content {
			if i < len(copy.Content) && len(copy.Content[i].Annotations) == 0 {
				copy.Content[i].Annotations = part.Annotations
			}
		}
		s.annotationItems[index] = &copy
		return nil
	}
	partIndex := 0
	if event.ContentIndex != nil {
		partIndex = *event.ContentIndex
	}
	if partIndex < 0 || partIndex > 1024 {
		return fmt.Errorf("invalid response content index")
	}
	for len(item.Content) <= partIndex {
		item.Content = append(item.Content, dto.ResponsesOutputContent{Type: "output_text"})
	}
	part := &item.Content[partIndex]
	switch event.Type {
	case "response.output_text.delta":
		part.Text += event.Delta
	case "response.output_text.done":
		if event.Text != nil {
			part.Text = *event.Text
		}
	case "response.output_text.annotation.added":
		var annotation map[string]any
		if err := kitutil.Unmarshal(event.Annotation, &annotation); err != nil {
			return fmt.Errorf("invalid response annotation: %w", err)
		}
		if annotation["type"] != "url_citation" {
			return fmt.Errorf("citation_source_unrepresentable: native citation requires its original protocol")
		}
		part.Annotations = append(part.Annotations, annotation)
	}
	return nil
}

func (s *ResponsesToChatStreamState) annotationsAtTerminal(response *dto.OpenAIResponsesResponse) *dto.OpenAIResponsesResponse {
	if response != nil && len(response.Output) > 0 {
		clone := *response
		clone.Output = append([]dto.ResponsesOutput(nil), response.Output...)
		for i := range clone.Output {
			if cached := s.annotationItems[i]; cached != nil {
				clone.Output[i].Content = append([]dto.ResponsesOutputContent(nil), clone.Output[i].Content...)
				for j := range clone.Output[i].Content {
					if j < len(cached.Content) && len(clone.Output[i].Content[j].Annotations) == 0 {
						clone.Output[i].Content[j].Annotations = cached.Content[j].Annotations
					}
				}
			}
		}
		return &clone
	}
	clone := &dto.OpenAIResponsesResponse{}
	if response != nil {
		*clone = *response
	}
	indexes := make([]int, 0, len(s.annotationItems))
	for index := range s.annotationItems {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		clone.Output = append(clone.Output, *s.annotationItems[index])
	}
	return clone
}
