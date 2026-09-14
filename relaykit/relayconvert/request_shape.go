package relayconvert

import (
	"fmt"
	"slices"
)

// RequestShape records only field presence and protocol structure. It contains
// no prompt, tool arguments, media location, or sampling parameter values.
type RequestShape struct {
	Fields   []string       `json:"fields"`
	Messages []MessageShape `json:"messages,omitempty"`
}

type MessageShape struct {
	Role    string             `json:"role"`
	Fields  []string           `json:"fields"`
	Content []ContentPartShape `json:"content,omitempty"`
}

type ContentPartShape struct {
	Type   string   `json:"type"`
	Fields []string `json:"fields,omitempty"`
}

func analyzeChatRequestShape(payload map[string]any) (*RequestShape, error) {
	shape := &RequestShape{Fields: presentFields(payload)}
	messages, _ := payload["messages"].([]any)
	for index, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("messages[%d] must be an object", index)
		}
		role, _ := message["role"].(string)
		entry := MessageShape{Role: role, Fields: presentFields(message)}
		switch content := message["content"].(type) {
		case nil:
		case string:
			entry.Content = []ContentPartShape{{Type: "text"}}
		case []any:
			for partIndex, item := range content {
				part, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("messages[%d].content[%d] must be an object", index, partIndex)
				}
				kind, _ := part["type"].(string)
				fields := presentFields(part)
				if image, ok := part["image_url"].(map[string]any); ok {
					for _, name := range presentFields(image) {
						fields = append(fields, "image_url."+name)
					}
				}
				entry.Content = append(entry.Content, ContentPartShape{Type: kind, Fields: fields})
			}
		default:
			return nil, fmt.Errorf("messages[%d].content must be a string, array or null", index)
		}
		shape.Messages = append(shape.Messages, entry)
	}
	return shape, nil
}

func presentFields(payload map[string]any) []string {
	fields := make([]string, 0, len(payload))
	for field, value := range payload {
		if value != nil {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)
	return fields
}
