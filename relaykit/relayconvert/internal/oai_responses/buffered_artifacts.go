package oairesponses

import (
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func (a *ResponsesBufferedAccumulator) Err() error { return a.err }

// Indexed events retain their native item identity until the final conversion.
// The legacy unindexed text path remains available for compatible providers.
func (a *ResponsesBufferedAccumulator) rememberIndexedEvent(event *dto.ResponsesStreamResponse) bool {
	switch event.Type {
	case "response.output_item.added", "response.output_item.done", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta", "response.function_call_arguments.done", "response.custom_tool_call_input.done", "response.output_text.delta", "response.output_text.done", "response.output_text.annotation.added", "response.content_part.added", "response.content_part.done", "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.done", "response.reasoning_text.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
	default:
		return false
	}
	itemID := event.ItemID
	if event.Item != nil && event.Item.ID != "" {
		itemID = event.Item.ID
	}
	index, known := a.itemIndexes[itemID]
	if event.OutputIndex != nil {
		if *event.OutputIndex < 0 || *event.OutputIndex > 1024 {
			a.err = fmt.Errorf("invalid response output index")
			return true
		}
		if known && index != *event.OutputIndex {
			if !a.unindexedItems[itemID] {
				a.err = fmt.Errorf("response item identity changed its output index")
				return true
			}
			// An ID-only delta may precede the provider's actual output index.
			// Move only provisional slots; fixed identities cannot be reassigned.
			previous, occupied := a.items[index], a.items[*event.OutputIndex]
			if occupied != nil && !a.unindexedItems[occupied.ID] {
				a.err = fmt.Errorf("response output index belongs to another item")
				return true
			}
			delete(a.items, index)
			if occupied != nil {
				a.items[index] = occupied
				a.itemIndexes[occupied.ID] = index
			}
			a.items[*event.OutputIndex] = previous
		}
		delete(a.unindexedItems, itemID)
		index, known = *event.OutputIndex, true
	}
	if !known && itemID != "" {
		for existing := range a.items {
			if existing >= index {
				index = existing + 1
			}
		}
		known = true
		a.unindexedItems[itemID] = true
	}
	if !known {
		if event.Type == "response.output_text.annotation.added" || event.Item != nil && !isResponsesToolOutputType(event.Item.Type) {
			a.err = fmt.Errorf("response item requires an output index or identity")
			return true
		}
		return false
	}
	if index < 0 || index > 1024 {
		a.err = fmt.Errorf("invalid response output index")
		return true
	}
	item := a.items[index]
	if item != nil && item.ID != "" && itemID != "" && item.ID != itemID {
		a.err = fmt.Errorf("response output index changed its item identity")
		return true
	}
	if item == nil {
		item = &dto.ResponsesOutput{ID: itemID}
		a.items[index] = item
	}
	if event.Item != nil {
		copy := *event.Item
		copy.Content = append([]dto.ResponsesOutputContent(nil), event.Item.Content...)
		copy.Summary = append([]dto.ResponsesReasoningSummaryPart(nil), event.Item.Summary...)
		mergeBufferedContent(&copy, item)
		if len(copy.Arguments) == 0 {
			copy.Arguments = item.Arguments
		}
		item = &copy
		a.items[index] = item
	}
	if item.ID != "" {
		a.itemIndexes[item.ID] = index
	}
	if event.ItemID != "" {
		a.itemIndexes[event.ItemID] = index
	}
	switch event.Type {
	case "response.output_item.added", "response.output_item.done":
		if event.Item == nil || item.Type == "" {
			a.err = fmt.Errorf("response output item is missing its type")
		}
		return true
	case "response.function_call_arguments.delta":
		item.Arguments, a.err = kitutil.Marshal(item.ArgumentsString() + event.Delta)
		return true
	case "response.custom_tool_call_input.delta":
		item.Input, a.err = kitutil.Marshal(dto.ResponsesArgumentsString(item.Input) + event.Delta)
		return true
	case "response.custom_tool_call_input.done":
		if event.Input != nil {
			item.Input, a.err = kitutil.Marshal(*event.Input)
		}
		return true
	case "response.function_call_arguments.done":
		if event.Arguments != nil {
			item.Arguments, a.err = kitutil.Marshal(*event.Arguments)
		}
		return true
	case "response.output_text.delta", "response.output_text.done", "response.output_text.annotation.added", "response.content_part.added", "response.content_part.done":
		item.Type, item.Role = "message", "assistant"
		partIndex := 0
		if event.ContentIndex != nil {
			partIndex = *event.ContentIndex
		}
		if partIndex < 0 || partIndex > 1024 {
			a.err = fmt.Errorf("invalid response content index")
			return true
		}
		for len(item.Content) <= partIndex {
			item.Content = append(item.Content, dto.ResponsesOutputContent{Type: "output_text"})
		}
		part := &item.Content[partIndex]
		if event.Type == "response.output_text.delta" {
			part.Text += event.Delta
		}
		if event.Text != nil {
			part.Text = *event.Text
		}
		if event.Part != nil {
			if event.Part.Text != "" {
				part.Text = event.Part.Text
			}
			if event.Part.Annotations != nil {
				part.Annotations = mergeBufferedAnnotations(part.Annotations, *event.Part.Annotations)
			}
		}
		if len(event.Annotation) > 0 {
			var annotation any
			if err := kitutil.Unmarshal(event.Annotation, &annotation); err != nil {
				a.err = err
				return true
			}
			part.Annotations = mergeBufferedAnnotations(part.Annotations, []any{annotation})
		}
		return true
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.done", "response.reasoning_text.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		item.Type = "reasoning"
		summaryIndex := 0
		if event.SummaryIndex != nil {
			summaryIndex = *event.SummaryIndex
		}
		if summaryIndex < 0 || summaryIndex > 1024 {
			a.err = fmt.Errorf("invalid response summary index")
			return true
		}
		for len(item.Summary) <= summaryIndex {
			item.Summary = append(item.Summary, dto.ResponsesReasoningSummaryPart{Type: "summary_text"})
		}
		item.Summary[summaryIndex].Text += event.Delta
		if event.Text != nil {
			item.Summary[summaryIndex].Text = *event.Text
		}
		if event.Part != nil && event.Part.Text != "" {
			item.Summary[summaryIndex].Text = event.Part.Text
		}
		return true
	}
	return false
}

func mergeBufferedContent(target, previous *dto.ResponsesOutput) {
	if target.ID == "" {
		target.ID = previous.ID
	}
	if target.Type == "" {
		target.Type = previous.Type
	}
	if target.Role == "" {
		target.Role = previous.Role
	}
	if target.Status == "" {
		target.Status = previous.Status
	}
	if target.Name == "" {
		target.Name = previous.Name
	}
	if target.CallId == "" {
		target.CallId = previous.CallId
	}
	if target.ServerLabel == "" {
		target.ServerLabel = previous.ServerLabel
	}
	if target.Result == "" {
		target.Result = previous.Result
	}
	if target.Quality == "" {
		target.Quality = previous.Quality
	}
	if target.Size == "" {
		target.Size = previous.Size
	}
	if len(target.Arguments) == 0 {
		target.Arguments = previous.Arguments
	}
	if len(target.Input) == 0 {
		target.Input = previous.Input
	}
	if len(target.Action) == 0 {
		target.Action = previous.Action
	}
	if len(target.Results) == 0 {
		target.Results = previous.Results
	}
	if len(target.Output) == 0 {
		target.Output = previous.Output
	}
	if len(target.ItemError) == 0 {
		target.ItemError = previous.ItemError
	}
	for i, part := range previous.Summary {
		if i >= len(target.Summary) {
			target.Summary = append(target.Summary, part)
			continue
		}
		if target.Summary[i].Type == "" {
			target.Summary[i].Type = part.Type
		}
		if target.Summary[i].Text == "" {
			target.Summary[i].Text = part.Text
		}
		if part.Annotations != nil {
			var annotations []any
			if target.Summary[i].Annotations != nil {
				annotations = *target.Summary[i].Annotations
			}
			merged := mergeBufferedAnnotations(annotations, *part.Annotations)
			target.Summary[i].Annotations = &merged
		}
	}
	for i, part := range previous.Content {
		if i >= len(target.Content) {
			target.Content = append(target.Content, part)
			continue
		}
		if target.Content[i].Text == "" {
			target.Content[i].Text = part.Text
		}
		if target.Content[i].Type == "" {
			target.Content[i].Type = part.Type
		}
		target.Content[i].Annotations = mergeBufferedAnnotations(target.Content[i].Annotations, part.Annotations)
	}
}

func mergeBufferedAnnotations(target, extra []any) []any {
	seen := make(map[string]bool, len(target))
	merged := make([]any, 0, len(target)+len(extra))
	for _, annotation := range append(append([]any(nil), target...), extra...) {
		raw, _ := kitutil.Marshal(annotation)
		if !seen[string(raw)] {
			merged = append(merged, annotation)
			seen[string(raw)] = true
		}
	}
	return merged
}

func (a *ResponsesBufferedAccumulator) supplementIndexedOutput(response *dto.OpenAIResponsesResponse) {
	for i, item := range response.Output {
		item.Content = append([]dto.ResponsesOutputContent(nil), item.Content...)
		item.Summary = append([]dto.ResponsesReasoningSummaryPart(nil), item.Summary...)
		index := i
		if known, ok := a.itemIndexes[item.ID]; ok {
			index = known
		} else if previous := a.items[index]; previous != nil && item.ID != "" && previous.ID != "" && item.ID != previous.ID {
			// A compact terminal array may omit earlier emitted items. Never
			// overwrite one of those items using the compact array's position.
			for existing := range a.items {
				if existing >= index {
					index = existing + 1
				}
			}
		}
		if previous := a.items[index]; previous != nil {
			mergeBufferedContent(&item, previous)
		}
		if item.ID != "" {
			a.itemIndexes[item.ID] = index
		}
		if item.Type == "" {
			a.err = fmt.Errorf("response output item is missing its type")
			return
		}
		a.items[index] = &item
	}
	if len(a.items) == 0 {
		return
	}
	indexes := make([]int, 0, len(a.items))
	for index := range a.items {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	response.Output = make([]dto.ResponsesOutput, 0, len(indexes))
	for _, index := range indexes {
		if a.items[index].Type == "" {
			a.err = fmt.Errorf("response output item is missing its type")
			response.Output = nil
			return
		}
		response.Output = append(response.Output, *a.items[index])
	}
}
