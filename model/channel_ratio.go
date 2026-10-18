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
	return c.validateRoutingFields()
}

func (c *Channel) validateRoutingFields() error {
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

var ChannelRuntimeOccupancy func(int) int

func (c *Channel) AfterFind(*gorm.DB) error {
	if ChannelRuntimeOccupancy != nil {
		c.InFlight = ChannelRuntimeOccupancy(c.Id)
	}
	return nil
}
