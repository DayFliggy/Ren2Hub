// Package modellab classifies display metadata. It never authorizes or routes a request.
package modellab

import (
	_ "embed"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"golang.org/x/text/unicode/norm"
)

//go:embed catalog.json
var catalogJSON []byte
var once sync.Once
var catalog struct {
	Version string `json:"version"`
	Labs    []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"labs"`
	Models  map[string]string `json:"models"`
	Aliases map[string]string `json:"aliases"`
}

type Classification struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

func load() {
	once.Do(func() {
		if err := common.Unmarshal(catalogJSON, &catalog); err != nil {
			panic("invalid embedded model lab catalog: " + err.Error())
		}
	})
}
func label(id, source string) Classification {
	load()
	for _, lab := range catalog.Labs {
		if lab.Slug == id {
			return Classification{ID: id, Name: lab.Name, Source: source}
		}
	}
	return Classification{ID: id, Name: id, Source: source}
}
func normalize(value string) string {
	return norm.NFKC.String(strings.ToLower(strings.TrimSpace(value)))
}

func ResolveModel(name, vendor string) Classification {
	load()
	if strings.TrimSpace(vendor) != "" {
		for _, lab := range catalog.Labs {
			if normalize(vendor) == normalize(lab.Name) || normalize(vendor) == lab.Slug {
				return label(lab.Slug, "vendor")
			}
		}
		return Classification{ID: "vendor:" + normalize(vendor), Name: vendor, Source: "vendor"}
	}
	normalized := normalize(name)
	if canonical, ok := catalog.Aliases[normalized]; ok {
		normalized = normalize(canonical)
	}
	if id, ok := catalog.Models[normalized]; ok {
		return label(id, "canonical")
	}
	for _, prefix := range []struct{ Prefix, ID string }{
		{"gpt-", "openai"}, {"o1", "openai"}, {"o3", "openai"}, {"o4", "openai"}, {"chatgpt-", "openai"}, {"dall-e", "openai"}, {"whisper-", "openai"}, {"text-embedding-", "openai"},
		{"claude-", "anthropic"}, {"gemini-", "google"}, {"gemma-", "google"}, {"imagen-", "google"}, {"veo-", "google"}, {"deepseek-", "deepseek"}, {"grok-", "xai"}, {"qwen", "alibaba"}, {"glm-", "zhipuai"}, {"kimi-", "moonshotai"}, {"moonshot-", "moonshotai"}, {"minimax-", "minimax"},
	} {
		if normalized == prefix.Prefix || strings.HasPrefix(normalized, strings.TrimSuffix(prefix.Prefix, "-")+"-") || (prefix.Prefix == "qwen" && strings.HasPrefix(normalized, "qwen") && len(normalized) > 4 && normalized[4] >= '0' && normalized[4] <= '9') {
			return label(prefix.ID, "alias")
		}
	}
	return label("unknown", "unknown")
}

// Only advertised names are classified. Mapping keys cannot add models to a
// directory, and a cycle or malformed map is reported rather than guessed.
func ResolveChannel(names []string, mapping string, vendors map[string]string) (Classification, error) {
	aliases := map[string]string{}
	if strings.TrimSpace(mapping) != "" {
		if err := common.UnmarshalJsonStr(mapping, &aliases); err != nil {
			return Classification{}, err
		}
	}
	found := map[string]Classification{}
	for _, name := range names {
		actual := name
		seen := map[string]bool{}
		for aliases[actual] != "" && aliases[actual] != actual {
			if seen[actual] {
				return label("unknown", "invalid_mapping"), nil
			}
			seen[actual] = true
			actual = aliases[actual]
		}
		vendor := vendors[name]
		if vendor == "" {
			vendor = vendors[actual]
		}
		item := ResolveModel(actual, vendor)
		if vendor == "" && actual != name && item.ID != "unknown" {
			item.Source = "mapping"
		}
		found[item.ID] = item
	}
	if len(found) == 0 {
		return label("unknown", "unknown"), nil
	}
	if len(found) > 1 {
		return label("mixed", "models"), nil
	}
	for _, item := range found {
		return item, nil
	}
	panic("unreachable model classification")
}
func Labs() []Classification {
	load()
	items := make([]Classification, 0, len(catalog.Labs))
	for _, lab := range catalog.Labs {
		items = append(items, label(lab.Slug, "catalog"))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
