package service

import (
	"math"
	"strconv"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// RouteAttemptEvidence is saved in the restricted request policy log. It contains
// measurements only; no provider payload, cache key or session ID is retained.
type RouteAttemptEvidence struct {
	Model           string  `json:"model"`
	Operation       int     `json:"operation"`
	Stream          bool    `json:"stream"`
	Outcome         string  `json:"outcome"`
	OutcomeReason   string  `json:"outcome_reason,omitempty"`
	DurationMS      int64   `json:"duration_ms"`
	FirstOutputMS   int64   `json:"first_output_ms,omitempty"`
	GenerationMS    int64   `json:"generation_ms,omitempty"`
	OutputTokens    int64   `json:"output_tokens,omitempty"`
	OutputSource    string  `json:"output_source"`
	UsageSource     string  `json:"usage_source,omitempty"`
	InputTokens     int64   `json:"input_tokens,omitempty"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CacheKnown      bool    `json:"cache_known"`
	CacheReason     string  `json:"cache_reason,omitempty"`
	CacheMode       string  `json:"cache_mode,omitempty"`
	Admission       string  `json:"admission,omitempty"`
	Quality         int     `json:"quality"`
	PriceFactor     float64 `json:"price_factor,omitempty"`
	Exploration     bool    `json:"exploration,omitempty"`
	Status          int     `json:"status,omitempty"`
	CooldownUntil   int64   `json:"cooldown_until_unix_ms,omitempty"`
}

type usageCount struct {
	value   int64
	present bool
}

type routeUsage struct {
	source                                     string
	responseState                              string
	input, output, read, write, tool, thinking usageCount
	conflict                                   bool
}

// Wire counts are cumulative. An absent field in a later SSE frame must
// preserve the earlier count, especially Claude message_start input usage.
func (u *routeUsage) merge(object gjson.Result, count *usageCount, aliases ...string) {
	var next usageCount
	for _, alias := range aliases {
		field := object.Get(alias)
		if !field.Exists() {
			continue
		}
		n, err := strconv.ParseInt(field.Raw, 10, 64)
		if err != nil || n < 0 || next.present && next.value != n {
			u.conflict = true
			return
		}
		next = usageCount{value: n, present: true}
	}
	if next.present {
		*count = next
	}
}

// ObserveRouteUsage accepts the actual wire dialect before conversion or token
// estimation. Reading the normalized DTO would turn omitted counts into zeros.
func ObserveRouteUsage(c *gin.Context, dialect string, body []byte) {
	if a := currentRouteAttempt(c); a != nil && a.bound {
		a.observeUsage(dialect, body)
	}
}

func (a *routeAttempt) observeUsage(dialect string, body []byte) {
	var object gjson.Result
	var responseState string
	switch dialect {
	case "openai":
		object = gjson.GetBytes(body, "usage")
		responseState = gjson.GetBytes(body, "status").String()
		if !object.IsObject() {
			object = gjson.GetBytes(body, "response.usage")
		}
	case "claude":
		object = gjson.GetBytes(body, "usage")
		if !object.IsObject() {
			object = gjson.GetBytes(body, "message.usage")
		}
	case "gemini":
		object = gjson.GetBytes(body, "usageMetadata")
	default:
		return
	}
	if !object.IsObject() && responseState == "" {
		return
	}
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	u := &a.usage
	if responseState != "" {
		// Keep only documented lifecycle states in restricted logs and memory.
		switch responseState {
		case "completed", "failed", "incomplete", "cancelled", "canceled", "queued", "in_progress":
			u.responseState = responseState
		}
	}
	if !object.IsObject() {
		return
	}
	if u.source != "" && u.source != dialect {
		u.conflict = true
		return
	}
	u.source = dialect
	switch dialect {
	case "openai":
		u.merge(object, &u.input, "prompt_tokens", "input_tokens")
		u.merge(object, &u.output, "completion_tokens", "output_tokens")
		u.merge(object, &u.read, "prompt_tokens_details.cached_tokens", "input_tokens_details.cached_tokens", "prompt_cache_hit_tokens", "cached_tokens")
	case "claude":
		u.merge(object, &u.input, "input_tokens")
		u.merge(object, &u.output, "output_tokens")
		u.merge(object, &u.read, "cache_read_input_tokens")
		u.merge(object, &u.write, "cache_creation_input_tokens")
	case "gemini":
		u.merge(object, &u.input, "promptTokenCount")
		u.merge(object, &u.output, "candidatesTokenCount")
		u.merge(object, &u.thinking, "thoughtsTokenCount")
		u.merge(object, &u.tool, "toolUsePromptTokenCount")
		u.merge(object, &u.read, "cachedContentTokenCount")
	}
}

func addRouteTokens(a, b int64) (int64, bool) {
	if b > math.MaxInt64-a {
		return 0, false
	}
	return a + b, true
}

func (a *routeAttempt) evidence(now time.Time, outcome string, stream bool) RouteAttemptEvidence {
	a.usageMu.Lock()
	u := a.usage
	a.usageMu.Unlock()
	if outcome == "success" {
		switch u.responseState {
		case "queued", "in_progress", "incomplete", "cancelled", "canceled":
			outcome = "ignored"
		case "failed":
			outcome = "failure"
		}
	}
	ev := RouteAttemptEvidence{
		Model: a.model, Operation: a.operation, Stream: stream, Outcome: outcome, DurationMS: now.Sub(a.started).Milliseconds(),
		UsageSource: u.source, OutputSource: "unknown", CacheReadTokens: u.read.value, Admission: a.admission,
		Exploration: a.admission == "explore", Quality: a.quality, PriceFactor: a.price, Status: int(a.status.Load()),
		CooldownUntil: a.cooldown.Load(), CacheMode: a.cacheMode,
	}
	first, last := a.firstOutput.Load(), a.lastOutput.Load()
	if first > 0 {
		ev.FirstOutputMS = max(1, time.Unix(0, first).Sub(a.started).Milliseconds())
		ev.GenerationMS = max(0, (last-first)/int64(time.Millisecond))
	}
	total, valid := addRouteTokens(u.input.value, u.tool.value)
	if u.source == "claude" && valid {
		total, valid = addRouteTokens(total, u.read.value)
		if valid {
			total, valid = addRouteTokens(total, u.write.value)
		}
	}
	ev.InputTokens = total
	if n, ok := addRouteTokens(u.output.value, u.thinking.value); ok && u.output.present && !u.conflict {
		ev.OutputSource = "upstream"
		ev.OutputTokens = n
	}
	switch {
	case outcome != "success":
		ev.CacheReason = "incomplete"
	case u.conflict || !valid || u.read.present && u.input.present && u.read.value > total:
		ev.CacheReason = "conflict"
	case !u.read.present:
		ev.CacheReason = "missing_read"
	case !u.input.present || total <= 0:
		ev.CacheReason = "missing_input"
	default:
		ev.CacheKnown = true
	}
	return ev
}

func (a *routeAttempt) finalEvidence(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, now time.Time) RouteAttemptEvidence {
	outcome, reason := a.routeOutcome(c, info, apiErr)
	stream := info.StreamStatus.OutcomeSnapshot()
	ev := a.evidence(now, string(outcome), info.IsStream)
	ev.OutcomeReason = reason
	if info.IsStream && info.StreamStatus == nil || stream.Response == relaycommon.ResponseOutcomeIncomplete || stream.Response == relaycommon.ResponseOutcomeCancelled {
		ev.CacheKnown, ev.CacheReason = false, "incomplete"
	}
	if ev.OutputSource == "unknown" && a.estimated.Load() {
		ev.OutputTokens, ev.OutputSource = a.estimatedCount.Load(), "estimated"
	} else if ev.OutputSource == "unknown" && info.PerformanceOutputTokens > 0 {
		ev.OutputTokens, ev.OutputSource = info.PerformanceOutputTokens, "unverified"
	}
	if apiErr != nil && ev.Status == 0 {
		ev.Status = apiErr.StatusCode
	}
	return ev
}

// Successful text settlement serializes the log before the controller finishes
// the attempt. Attach a snapshot to the copy; on failure FinishRouteAttempt
// publishes the final observation before the error log is assembled.
func routePolicyEventsForLog(c *gin.Context) []PolicyEvent {
	state := RequestPolicy(c)
	events := state.Events()
	if a := currentRouteAttempt(c); a != nil && a.bound && a.info != nil {
		ev := a.finalEvidence(c, a.info, nil, time.Now())
		attachRouteEvidence(events, state.Attempts, a.channelID, &ev)
	}
	return events
}

func attachRouteEvidence(events []PolicyEvent, number, id int, ev *RouteAttemptEvidence) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Attempt == number && events[i].ChannelID == id && events[i].Decision.Action == "attempt" {
			events[i].RouteAttempt = ev
			return
		}
	}
}

func (ev RouteAttemptEvidence) speedSample() (int64, time.Duration) {
	generation := time.Duration(ev.GenerationMS) * time.Millisecond
	if !ev.Stream || ev.Outcome != "success" || ev.CacheReason == "incomplete" ||
		ev.OutputSource != "upstream" || ev.OutputTokens < 50 || generation < 500*time.Millisecond {
		return 0, 0
	}
	return ev.OutputTokens, generation
}
