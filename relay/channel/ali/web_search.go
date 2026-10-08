package ali

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/tidwall/gjson"
)

// aliSearchAgentKey is the DashScope web-search tool-price key. The Responses
// API "bills web search like the agent strategy"; the Anthropic-compatible
// endpoint publishes no search price and is billed like agent too.
const aliSearchAgentKey = "search_strategy_agent"

// aliWebSearchToolPriceKeys lists, per declared hosted tool type prefix, the
// price key DashScope bills for web search on its Responses and
// Anthropic-compatible endpoints.
var aliWebSearchToolPriceKeys = map[string][]string{
	dto.BuildInToolWebSearch: {aliSearchAgentKey},
}

// aliResponsesToolUsage bills Responses web search by the count DashScope
// reports in usage.x_tools.web_search.count (terminal frames carry it under
// response.usage) and silences the web_search_call items under the protocol
// keys. gjson truncates fractional values and reads non-numbers as zero;
// SetBillableToolCount ignores negatives and settlement applies
// MaxBillableToolCallCount.
func aliResponsesToolUsage(raw []byte) map[string]int {
	count := gjson.GetBytes(raw, "usage.x_tools.web_search.count")
	if !count.Exists() {
		count = gjson.GetBytes(raw, "response.usage.x_tools.web_search.count")
	}
	if !count.Exists() {
		return nil
	}
	return map[string]int{
		aliSearchAgentKey:               int(count.Int()),
		dto.BuildInToolWebSearch:        0,
		dto.BuildInToolWebSearchPreview: 0,
	}
}

// aliAnthropicWebSearchEntrypoint is the client marker DashScope's
// Anthropic-compatible endpoint requires in system before it runs web search:
// "使用 Anthropic SDK 或 HTTP 直接调用时，需在 system 中传入客户端标识
// x-anthropic-billing-header: cc_entrypoint=cli;，否则联网搜索不生效。"
// (https://help.aliyun.com/zh/model-studio/web-search). The marker must be a
// system text block: sent as a plain string system, qwen3.8-flash answered
// without searching (2026-10-05).
const aliAnthropicWebSearchEntrypoint = "x-anthropic-billing-header: cc_entrypoint=cli;"

// ensureAnthropicWebSearchEntrypoint prepends the marker block to system when
// the request carries a web_search_* tool; a string system becomes the block
// after the marker. It is idempotent: a system that already names
// cc_entrypoint=cli, from the client or an earlier call, is left as is.
func ensureAnthropicWebSearchEntrypoint(request *dto.ClaudeRequest) {
	hasWebSearch := false
	for _, tool := range request.GetTools() {
		var toolType string
		switch typed := tool.(type) {
		case map[string]any:
			toolType, _ = typed["type"].(string)
		case *dto.ClaudeWebSearchTool:
			toolType = typed.Type
		case dto.ClaudeWebSearchTool:
			toolType = typed.Type
		}
		if strings.HasPrefix(toolType, "web_search_") {
			hasWebSearch = true
			break
		}
	}
	if !hasWebSearch {
		return
	}
	const present = "cc_entrypoint=cli"
	var blocks []dto.ClaudeMediaMessage
	if request.System == nil || request.IsStringSystem() {
		system := request.GetStringSystem()
		if strings.Contains(system, present) {
			return
		}
		if strings.TrimSpace(system) != "" {
			block := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
			block.SetText(system)
			blocks = []dto.ClaudeMediaMessage{block}
		}
	} else {
		blocks = request.ParseSystem()
		for _, block := range blocks {
			if block.Type == dto.ContentTypeText && strings.Contains(block.GetText(), present) {
				return
			}
		}
	}
	marker := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
	marker.SetText(aliAnthropicWebSearchEntrypoint)
	request.System = append([]dto.ClaudeMediaMessage{marker}, blocks...)
}
