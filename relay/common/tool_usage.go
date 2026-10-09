package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// GoogleSearchGroundedPromptTool bills Gemini 2.5-and-older Google Search
// grounding, charged once per grounded prompt.
const GoogleSearchGroundedPromptTool = "google_search_grounded_prompt"

// reservedBillableToolNames are hosted-tool keys without a built-in price
// (Anthropic server tools reported in usage.server_tool_use). They and every
// built-in hosted-tool price key (operation_setting.IsBuiltInToolPriceKey)
// bill hosted tools; a client function or tool_use with the same name is
// never counted under them.
var reservedBillableToolNames = map[string]struct{}{
	"web_fetch":      {},
	"code_execution": {},
	"tool_search":    {},
}

// MaxBillableToolCallCount bounds every tool count at settlement, whatever its
// source, before it becomes a surcharge multiplier.
const MaxBillableToolCallCount = 10_000

type billableToolIdentity struct {
	id, callID, kind, name string
}

// CountBillableToolCall is the single entry point for per-call tool billing counts.
// Built-in call types always count; custom function/tool_use names only count when priced.
func (info *RelayInfo) CountBillableToolCall(itemType string, functionName string) {
	if info == nil {
		return
	}
	info.ensureBuiltInTools()

	switch itemType {
	case dto.BuildInCallWebSearchCall:
		info.incrementBillableToolCall(resolveWebSearchToolName(info.ResponsesUsageInfo.BuiltInTools))
	case dto.BuildInCallFileSearchCall:
		info.incrementBillableToolCall(dto.BuildInToolFileSearch)
	case dto.BuildInCallFunctionCall, dto.BuildInCallToolUse:
		if functionName == "" {
			return
		}
		if _, reserved := reservedBillableToolNames[functionName]; reserved || operation_setting.IsBuiltInToolPriceKey(functionName) {
			return
		}
		// Ren2Hub-local: the frozen quote, not the live price table, decides.
		if !info.QuotePricesTool(functionName) {
			return
		}
		info.incrementBillableToolCall(functionName)
	}
}

// QuotePricesTool reports whether the frozen quote carries a price for the
// tool key, including an explicit zero.
func (info *RelayInfo) QuotePricesTool(name string) bool {
	if info == nil || info.PriceData.Quote == nil {
		return false
	}
	for _, price := range info.PriceData.Quote.PriceVersion.Prices {
		if price.Quantity == "tool:"+name {
			return true
		}
	}
	return false
}

// ObserveBillableToolCall records a confirmed output item once across item.done
// events and the cumulative terminal response. Declarations never call this.
func (info *RelayInfo) ObserveBillableToolCall(item *dto.ResponsesOutput, index *int) {
	if info == nil || item == nil {
		return
	}
	switch item.Type {
	case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall, dto.BuildInCallFunctionCall:
	default:
		return
	}
	switch item.Status {
	case "failed", "cancelled", "canceled", "incomplete", "in_progress":
		return
	}
	aliases := []string{}
	if item.ID != "" {
		aliases = append(aliases, "id:"+item.ID)
	}
	if item.CallId != "" {
		aliases = append(aliases, "call:"+item.CallId)
	}
	if index != nil && *index >= 0 {
		aliases = append(aliases, fmt.Sprintf("index:%d", *index))
	}
	if len(aliases) == 0 {
		info.BillingToolUsageInvalid = true
		return
	}
	if info.BillingToolCallIDs == nil {
		info.BillingToolCallIDs = map[string]*billableToolIdentity{}
	}
	var previous *billableToolIdentity
	for _, key := range aliases {
		observed := info.BillingToolCallIDs[key]
		if observed == nil {
			continue
		}
		if previous != nil && previous != observed || observed.kind != item.Type || observed.name != item.Name ||
			observed.id != "" && item.ID != "" && observed.id != item.ID || observed.callID != "" && item.CallId != "" && observed.callID != item.CallId {
			info.BillingToolUsageInvalid = true
			return
		}
		previous = observed
	}
	seen := previous != nil
	if previous == nil {
		previous = &billableToolIdentity{kind: item.Type, name: item.Name}
	}
	if item.ID != "" {
		previous.id = item.ID
	}
	if item.CallId != "" {
		previous.callID = item.CallId
	}
	for _, key := range aliases {
		info.BillingToolCallIDs[key] = previous
	}
	if !seen {
		info.CountBillableToolCall(item.Type, item.Name)
	}
}

// SetBillableToolCount records an upstream-reported count for one tool-price
// key, replacing whatever per-item counting produced for the same key. Zero is
// a valid value: a vendor field that reports zero suppresses the protocol items
// counted under that key. Negative counts are ignored.
func (info *RelayInfo) SetBillableToolCount(name string, count int) {
	if info == nil || name == "" {
		return
	}
	if count < 0 {
		common.SysError(fmt.Sprintf("billable tool count ignored: tool=%s count=%d", name, count))
		return
	}
	info.ensureBuiltInTools()
	if existing, ok := info.ResponsesUsageInfo.BuiltInTools[name]; ok && existing != nil {
		existing.CallCount = count
		return
	}
	info.ResponsesUsageInfo.BuiltInTools[name] = &BuildInToolInfo{ToolName: name, CallCount: count}
}

// ApplyVendorToolUsage writes the vendor-billed counts of one upstream
// response into BuiltInTools. Protocol items counted earlier for the same key
// are replaced; keys the vendor does not mention are kept. Keys it creates
// belong to the current channel attempt (see InitChannelMeta).
func (info *RelayInfo) ApplyVendorToolUsage(raw []byte) {
	if info == nil || info.VendorToolUsage == nil || len(raw) == 0 {
		return
	}
	info.ensureBuiltInTools()
	for name, count := range info.VendorToolUsage(raw) {
		_, existed := info.ResponsesUsageInfo.BuiltInTools[name]
		info.SetBillableToolCount(name, count)
		if _, exists := info.ResponsesUsageInfo.BuiltInTools[name]; exists && !existed {
			info.vendorToolKeys = append(info.vendorToolKeys, name)
		}
	}
}

func (info *RelayInfo) ensureBuiltInTools() {
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &ResponsesUsageInfo{}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*BuildInToolInfo)
	}
}

func resolveWebSearchToolName(tools map[string]*BuildInToolInfo) string {
	if _, ok := tools[dto.BuildInToolWebSearchPreview]; ok {
		return dto.BuildInToolWebSearchPreview
	}
	if _, ok := tools[dto.BuildInToolWebSearch]; ok {
		return dto.BuildInToolWebSearch
	}
	return dto.BuildInToolWebSearchPreview
}

func (info *RelayInfo) incrementBillableToolCall(name string) {
	if existing, ok := info.ResponsesUsageInfo.BuiltInTools[name]; ok && existing != nil {
		existing.CallCount++
		return
	}
	info.ResponsesUsageInfo.BuiltInTools[name] = &BuildInToolInfo{
		ToolName:  name,
		CallCount: 1,
	}
}

// ImageGenerationCallCounter counts completed Responses image_generation_call
// outputs with stream-safe identity deduplication.
type ImageGenerationCallCounter struct {
	seen  map[string]struct{}
	count int
}

// Observe records one completed final image output when billable.
// outputIndex may be nil; when set and nonnegative it participates in dedup.
func (c *ImageGenerationCallCounter) Observe(item *dto.ResponsesOutput, outputIndex *int) {
	if c == nil || item == nil {
		return
	}
	if item.Type != dto.ResponsesOutputTypeImageGenerationCall {
		return
	}
	if strings.TrimSpace(item.Result) == "" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "failed", "cancelled", "canceled", "incomplete", "partial":
		return
	}

	aliases := make([]string, 0, 4)
	if item.ID != "" {
		aliases = append(aliases, "id:"+item.ID)
	}
	if item.CallId != "" {
		aliases = append(aliases, "call:"+item.CallId)
	}
	if outputIndex != nil && *outputIndex >= 0 {
		aliases = append(aliases, fmt.Sprintf("index:%d", *outputIndex))
	}
	if len(aliases) == 0 {
		sum := sha256.Sum256([]byte(item.Result))
		aliases = append(aliases, "result:"+hex.EncodeToString(sum[:]))
	}

	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	for _, alias := range aliases {
		if _, ok := c.seen[alias]; ok {
			return
		}
	}
	for _, alias := range aliases {
		c.seen[alias] = struct{}{}
	}
	c.count++
}

// Count returns the deduplicated completed image output count before commit capping.
func (c *ImageGenerationCallCounter) Count() int {
	if c == nil {
		return 0
	}
	return c.count
}

// Commit writes the capped completed-output count into RelayInfo once.
// Request tool declarations alone must not become billable calls.
func (c *ImageGenerationCallCounter) Commit(info *RelayInfo) {
	if info == nil {
		return
	}
	info.ensureBuiltInTools()

	count := 0
	if c != nil {
		count = c.count
	}
	if count > dto.MaxImageN {
		count = dto.MaxImageN
	}

	if existing, ok := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]; ok && existing != nil {
		existing.CallCount = count
		return
	}
	info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration] = &BuildInToolInfo{
		ToolName:  dto.BuildInToolImageGeneration,
		CallCount: count,
	}
}

// IsNonBillableResponsesStatus reports response statuses that do not complete
// the request. Such a terminal does not confirm completed usage, so its output
// snapshot adds no tool calls; calls and images confirmed earlier in the
// stream stay billed.
func IsNonBillableResponsesStatus(status []byte) bool {
	if len(status) == 0 {
		return false
	}
	var s string
	if err := common.Unmarshal(status, &s); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "failed", "cancelled", "canceled", "incomplete":
		return true
	default:
		return false
	}
}
