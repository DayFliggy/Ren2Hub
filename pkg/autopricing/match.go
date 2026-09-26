package autopricing

import (
	"regexp"
	"strings"
	"time"
)

var dateSuffix = regexp.MustCompile(`^(.*?)-(\d{4}-\d{2}-\d{2}|\d{8})$`)
var googleResource = regexp.MustCompile(`^(?:projects/[^/]+/locations/[^/]+/)?publishers/google/models/(.+)$`)

// Resolve prefers an exact key. Aliases are limited to documented resource
// wrappers and valid calendar-date suffixes to an existing base identity.
func Resolve(entries map[string]Entry, model string) (Entry, bool) {
	model = strings.TrimSpace(model)
	if entry, exists := entries[model]; exists {
		return cloneEntry(entry), true
	}
	canonical := resourceModel(model)
	if canonical != model {
		if entry, exists := entries[canonical]; exists {
			return cloneEntry(entry), true
		}
	}
	base := withoutDate(canonical)
	if base != canonical {
		if entry, exists := entries[base]; exists {
			return cloneEntry(entry), true
		}
	}
	return Entry{}, false
}

// IsBlocked follows Resolve's one-way aliases within a source. A rejected dated
// identity cannot block its base or a different date; another source may still
// supply a complete accepted price.
func IsBlocked(rejected map[string]bool, model string) bool {
	model = strings.TrimSpace(model)
	if rejected[model] {
		return true
	}
	canonical := resourceModel(model)
	base := withoutDate(canonical)
	for key, blocked := range rejected {
		identity := resourceModel(strings.TrimSpace(key))
		if blocked && (identity == canonical || identity == base) {
			return true
		}
	}
	return false
}

func resourceModel(model string) string {
	if match := googleResource.FindStringSubmatch(model); match != nil {
		return match[1]
	}
	return strings.TrimPrefix(model, "models/")
}

func withoutDate(model string) string {
	match := dateSuffix.FindStringSubmatch(model)
	if match == nil || match[1] == "" {
		return model
	}
	format := "20060102"
	if len(match[2]) == 10 {
		format = "2006-01-02"
	}
	if _, err := time.Parse(format, match[2]); err != nil {
		return model
	}
	return match[1]
}
