// Adapted from new-api 7209b6db95ef; retained under the repository license.
package oaichat

import (
	"encoding/json"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func chatAnnotationsToClaude(raw json.RawMessage, text string) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var annotations []map[string]any
	if err := kitutil.Unmarshal(raw, &annotations); err != nil {
		return nil
	}
	citations := make([]json.RawMessage, 0, len(annotations))
	for _, annotation := range annotations {
		if strings.TrimSpace(kitutil.Interface2String(annotation["type"])) != "url_citation" {
			continue
		}
		citation, ok := annotation["url_citation"].(map[string]any)
		if !ok {
			citation = annotation
		}
		url := strings.TrimSpace(kitutil.Interface2String(citation["url"]))
		if url == "" {
			continue
		}
		converted := map[string]any{
			"type":  "web_search_result_location",
			"url":   url,
			"title": strings.TrimSpace(kitutil.Interface2String(citation["title"])),
		}
		if citedText := kitutil.Interface2String(citation["cited_text"]); citedText != "" {
			converted["cited_text"] = citedText
		}
		if encryptedIndex := kitutil.Interface2String(citation["encrypted_index"]); encryptedIndex != "" {
			converted["encrypted_index"] = encryptedIndex
		}
		if converted["title"] == "" {
			delete(converted, "title")
		}
		encoded, err := kitutil.Marshal(converted)
		if err == nil {
			citations = append(citations, encoded)
		}
	}
	return citations
}

func claudeCitationHasSource(citation map[string]any) bool {
	quote, _ := citation["cited_text"].(string)
	reference, _ := citation["encrypted_index"].(string)
	return quote != "" && reference != ""
}

func annotationIndex(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), number >= 0 && number == float64(int(number))
	case int:
		return number, number >= 0
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err == nil && parsed >= 0
	default:
		return 0, false
	}
}
