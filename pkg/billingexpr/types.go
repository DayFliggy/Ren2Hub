package billingexpr

import (
	"crypto/sha256"
	"fmt"
	"time"
)

type RequestInput struct {
	// Raw payload fields remain source-compatible during adapter refactoring;
	// the evaluator never reads or persists them.
	Headers     map[string]string      `json:"-"`
	Body        []byte                 `json:"-"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	FrozenAt    time.Time              `json:"frozen_at"`
	Usage       map[string]interface{} `json:"usage,omitempty"`
	UsageSchema *UsageSchema           `json:"usage_schema,omitempty"`
	ImageCount  *int                   `json:"image_count,omitempty"`
}

// TokenParams holds all token dimensions passed into an Expr evaluation.
// Fields beyond P and C are optional — when absent they default to 0,
// which means cache-unaware expressions keep working unchanged.
type TokenParams struct {
	P         float64 // prompt tokens (text) — auto-excludes sub-categories priced separately
	C         float64 // completion tokens (text) — auto-excludes sub-categories priced separately
	Len       float64 // total input context length for tier conditions (non-Claude: raw prompt_tokens; Claude: text + cache read + cache creation)
	CR        float64 // cache read (hit) tokens
	CC        float64 // cache creation tokens (5-min TTL for Claude, generic for others)
	CC1h      float64 // cache creation tokens — 1-hour TTL (Claude only)
	Img       float64 // image input tokens
	ImgCR     float64 // cached image input tokens when explicitly priced
	ImgO      float64 // image output tokens
	AI        float64 // audio input tokens
	AO        float64 // audio output tokens
	Reasoning float64 // reasoning output tokens, when independently priced
}

// RequestRuleTrace describes one request-dependent multiplier detected at compile time.
type RequestRuleTrace struct {
	Cond       string `json:"cond"`
	Multiplier string `json:"multiplier"`
	Matched    bool   `json:"matched"`
}

// TraceResult holds side-channel info captured while an expression runs.
type TraceResult struct {
	MatchedTier  string             `json:"matched_tier"`
	ImageCount   *int               `json:"image_count,omitempty"`
	RequestRules []RequestRuleTrace `json:"request_rules,omitempty"`
}

// ExprHashString returns the SHA-256 hex digest of an expression string.
func ExprHashString(expr string) string {
	h := sha256.Sum256([]byte(expr))
	return fmt.Sprintf("%x", h)
}
