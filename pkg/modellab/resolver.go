// Package modellab 只作为 NewAPI 厂商词表的补充：原生 defaultVendorRules 认不出的
// 模型名，用这里的目录和前缀推断厂商，结果仍是 NewAPI 厂商名。它不参与鉴权和选路。
package modellab

import (
	_ "embed"
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

// Classification 是渠道或模型的展示分类。ID 是稳定的分组键，Name 是 NewAPI 厂商名。
type Classification struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

// newAPIVendorNames 把目录里的实验室映射到原生 defaultVendorRules 使用的厂商名。
// 不在表中的实验室使用目录显示名，只有数据库里存在同名厂商时才会被采用。
var newAPIVendorNames = map[string]string{
	"openai": "OpenAI", "anthropic": "Anthropic", "google": "Google", "deepseek": "DeepSeek",
	"alibaba": "阿里巴巴", "moonshotai": "Moonshot", "zhipuai": "智谱", "minimax": "MiniMax",
	"mistral": "Mistral", "xai": "xAI", "meta": "Meta", "cohere": "Cohere",
	"bytedance-seed": "字节跳动", "tencent": "腾讯", "microsoft": "微软",
}

var prefixLabs = []struct{ Prefix, ID string }{
	{"gpt-", "openai"}, {"o1", "openai"}, {"o3", "openai"}, {"o4", "openai"}, {"chatgpt-", "openai"}, {"dall-e", "openai"}, {"whisper-", "openai"}, {"text-embedding-", "openai"},
	{"claude-", "anthropic"}, {"gemini-", "google"}, {"gemma-", "google"}, {"imagen-", "google"}, {"veo-", "google"}, {"deepseek-", "deepseek"}, {"grok-", "xai"}, {"qwen", "alibaba"}, {"glm-", "zhipuai"}, {"kimi-", "moonshotai"}, {"moonshot-", "moonshotai"}, {"minimax-", "minimax"},
}

func load() {
	once.Do(func() {
		if err := common.Unmarshal(catalogJSON, &catalog); err != nil {
			panic("invalid embedded model lab catalog: " + err.Error())
		}
	})
}

func normalize(value string) string {
	return norm.NFKC.String(strings.ToLower(strings.TrimSpace(value)))
}

// VendorName 返回补充推断出的 NewAPI 厂商名；推断不出时返回空串。
func VendorName(modelName string) string {
	slug := labSlug(modelName)
	if slug == "" {
		return ""
	}
	if name := newAPIVendorNames[slug]; name != "" {
		return name
	}
	for _, lab := range catalog.Labs {
		if lab.Slug == slug {
			return lab.Name
		}
	}
	return ""
}

func labSlug(modelName string) string {
	load()
	normalized := normalize(modelName)
	if canonical, ok := catalog.Aliases[normalized]; ok {
		normalized = normalize(canonical)
	}
	if id, ok := catalog.Models[normalized]; ok {
		return id
	}
	for _, prefix := range prefixLabs {
		if normalized == prefix.Prefix || strings.HasPrefix(normalized, strings.TrimSuffix(prefix.Prefix, "-")+"-") || (prefix.Prefix == "qwen" && strings.HasPrefix(normalized, "qwen") && len(normalized) > 4 && normalized[4] >= '0' && normalized[4] <= '9') {
			return prefix.ID
		}
	}
	return ""
}

// VendorID 给 NewAPI 厂商名一个稳定的分组键：与目录实验室对应的厂商沿用实验室 slug，
// 前端按它取图标；其他厂商为 "vendor:" 加规范化名称。
func VendorID(vendorName string) string {
	load()
	normalized := normalize(vendorName)
	for slug, name := range newAPIVendorNames {
		if normalize(name) == normalized {
			return slug
		}
	}
	for _, lab := range catalog.Labs {
		if normalize(lab.Name) == normalized || lab.Slug == normalized {
			return lab.Slug
		}
	}
	return "vendor:" + normalized
}
