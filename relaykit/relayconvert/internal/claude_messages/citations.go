// Adapted from new-api 7209b6db95ef; retained under the repository license.
package claudemessages

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func claudeCitationsToChat(raw json.RawMessage, text string, textOffset int) ([]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var citations []map[string]any
	if err := kitutil.Unmarshal(raw, &citations); err != nil {
		return nil, fmt.Errorf("invalid Claude citations: %w", err)
	}
	annotations := make([]any, 0, len(citations))
	for _, citation := range citations {
		if reference, _ := citation["encrypted_index"].(string); reference != "" {
			return nil, fmt.Errorf("native_state_required: encrypted citation reference requires the original protocol")
		}
		url := strings.TrimSpace(kitutil.Interface2String(citation["url"]))
		if url == "" {
			return nil, fmt.Errorf("citation_source_unrepresentable: native document citation requires its original protocol")
		}
		converted := map[string]any{
			"url":   url,
			"title": strings.TrimSpace(kitutil.Interface2String(citation["title"])),
		}
		citedText := kitutil.Interface2String(citation["cited_text"])
		if citedText != "" {
			converted["cited_text"] = citedText

		}
		// Claude attaches citations to the enclosing output text block; cited_text
		// is a source quotation, not an offset into the generated answer.
		if text != "" {
			converted["start_index"] = textOffset
			converted["end_index"] = textOffset + utf8.RuneCountInString(text)
		}
		if encryptedIndex := kitutil.Interface2String(citation["encrypted_index"]); encryptedIndex != "" {
			converted["encrypted_index"] = encryptedIndex
		}
		if converted["title"] == "" {
			delete(converted, "title")
		}
		annotations = append(annotations, map[string]any{
			"type":         "url_citation",
			"url_citation": converted,
		})
	}
	return annotations, nil
}

func marshalChatAnnotations(annotations []any) (json.RawMessage, error) {
	if len(annotations) == 0 {
		return nil, nil
	}
	return kitutil.Marshal(annotations)
}
