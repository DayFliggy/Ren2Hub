package billingexpr

import (
	"crypto/sha256"
	"fmt"
	"time"
)

type RequestInput struct {
	Headers     map[string]string      `json:"headers,omitempty"`
	Body        []byte                 `json:"body,omitempty"`
	Evaluation  *Evaluation            `json:"-"`
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
	P         int64 // prompt tokens (text) — auto-excludes sub-categories priced separately
	C         int64 // completion tokens (text) — auto-excludes sub-categories priced separately
	Len       int64 // total input context length for tier conditions (non-Claude: raw prompt_tokens; Claude: text + cache read + cache creation)
	CR        int64 // cache read (hit) tokens
	CC        int64 // cache creation tokens (5-min TTL for Claude, generic for others)
	CC1h      int64 // cache creation tokens — 1-hour TTL (Claude only)
	Img       int64 // image input tokens
	ImgCR     int64 // cached image input tokens when explicitly priced
	ImgO      int64 // image output tokens
	AI        int64 // audio input tokens
	AICR      int64 // cached audio input tokens when explicitly priced
	AO        int64 // audio output tokens
	Reasoning int64 // reasoning output tokens, when independently priced
}

// RequestRuleTrace describes one request-dependent multiplier detected at compile time.
type RequestRuleTrace struct {
	Cond       string `json:"cond"`
	Multiplier string `json:"multiplier"`
	Matched    bool   `json:"matched"`
}

// TraceResult holds side-channel info captured while an expression runs.
type TraceResult struct {
	BillingUnit  BillingUnit        `json:"billing_unit"`
	FixedPrice   *float64           `json:"fixed_price,omitempty"`
	Cost         float64            `json:"cost,omitempty"`
	MatchedTier  string             `json:"matched_tier"`
	ImageCount   *int               `json:"image_count,omitempty"`
	RequestRules []RequestRuleTrace `json:"request_rules,omitempty"`
}

type BillingUnit string

const (
	BillingUnitToken   BillingUnit = "token"
	BillingUnitRequest BillingUnit = "request"
)

// ExprHashString returns the SHA-256 hex digest of an expression string.
func ExprHashString(expr string) string {
	h := sha256.Sum256([]byte(expr))
	return fmt.Sprintf("%x", h)
}
