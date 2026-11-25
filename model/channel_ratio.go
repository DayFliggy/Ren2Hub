package model

import (
	"errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ParseBoundedRatio parses a ratio string and accepts at most 18 decimal places
// and a decimal exponent of at most 18. The exponent is checked before any
// comparison: decimal.Cmp rescales both operands to the smaller exponent, so a
// short input such as "1e2147483647" would otherwise build a huge big.Int.
// Callers apply their own sign and maximum on the returned value.
func ParseBoundedRatio(value string) (decimal.Decimal, bool) {
	if len(value) == 0 || len(value) > 64 {
		return decimal.Zero, false
	}
	r, err := decimal.NewFromString(value)
	if err != nil || r.Exponent() < -18 || r.Exponent() > 18 {
		return decimal.Zero, false
	}
	return r, true
}

func NormalizeChannelRatio(value string) (string, error) {
	r, ok := ParseBoundedRatio(value)
	if !ok || !r.IsPositive() || r.GreaterThan(decimal.NewFromInt(1000)) {
		return "", errors.New("channel ratio must be greater than 0 and at most 1000, with at most 18 decimal places")
	}
	return r.String(), nil
}

func (c *Channel) BeforeCreate(*gorm.DB) error {
	if c.ChannelRatio == "" {
		c.ChannelRatio = "1"
	}
	return c.ValidateRoutingFields()
}

// ValidateRoutingFields 是 channel_ratio 和 max_concurrency 唯一的校验，
// 同时把 channel_ratio 规范化，调用方写库的是规范值。
func (c *Channel) ValidateRoutingFields() error {
	if c.MaxConcurrency < 0 {
		return errors.New("max_concurrency must be nonnegative")
	}
	r, err := NormalizeChannelRatio(c.ChannelRatio)
	if err != nil {
		return err
	}
	c.ChannelRatio = r
	return nil
}

// ChannelRoutingChanged publishes committed admission settings and invalidates old observations.
var ChannelRoutingChanged func(int) error

// ChannelRoutingDeleted invalidates observations immediately after committed channel deletion.
var ChannelRoutingDeleted func(ids []int)

// ChannelRoutingMutating marks channels as having a configuration mutation in
// flight before the database transaction commits. Callers defer the returned
// release func so the pending mark covers commit through post-commit publication.
var ChannelRoutingMutating func(ids []int) func()

// BeginChannelRoutingMutation marks channels as mutating across DB commit and
// post-commit publication. Safe to call when no hook is registered.
func BeginChannelRoutingMutation(ids ...int) func() {
	if ChannelRoutingMutating == nil || len(ids) == 0 {
		return func() {}
	}
	return ChannelRoutingMutating(ids)
}

func beginChannelRoutingMutation(ids ...int) func() {
	return BeginChannelRoutingMutation(ids...)
}

// publishChannelRoutingIDs 只在提交后发布本次实际修改的渠道。
// 首个渠道失败时必须继续遍历其余已提交渠道，确保所有渠道均进入待发布或失效态。
func publishChannelRoutingIDs(ids []int) error {
	if ChannelRoutingChanged == nil {
		return nil
	}
	var firstErr error
	for _, id := range ids {
		if err := ChannelRoutingChanged(id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ChannelRoutingQuality is a read-only management projection of the worst evidenced model band.
type ChannelRoutingQuality struct {
	Model  string `json:"model"`
	Band   int    `json:"band"`
	Reason string `json:"reason"`
}
