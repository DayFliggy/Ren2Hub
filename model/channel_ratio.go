package model

import (
	"errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func NormalizeChannelRatio(value string) (string, error) {
	if len(value) == 0 || len(value) > 64 {
		return "", errors.New("invalid channel ratio")
	}
	r, err := decimal.NewFromString(value)
	if err != nil || !r.IsPositive() || r.GreaterThan(decimal.NewFromInt(1000)) || r.Exponent() < -18 || r.Exponent() > 18 {
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
