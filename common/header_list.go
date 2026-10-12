package common

import "strings"

// NormalizeHeaderListValues reads every line of a comma-separated list header
// (RFC 9110 section 5.6.1) and returns its items trimmed, without empty items,
// and deduplicated in first-seen order.
func NormalizeHeaderListValues(values []string) []string {
	normalizedValues := make([]string, 0, len(values))
	seenValues := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			normalizedItem := strings.TrimSpace(item)
			if normalizedItem == "" {
				continue
			}
			if _, exists := seenValues[normalizedItem]; exists {
				continue
			}
			seenValues[normalizedItem] = struct{}{}
			normalizedValues = append(normalizedValues, normalizedItem)
		}
	}
	return normalizedValues
}
