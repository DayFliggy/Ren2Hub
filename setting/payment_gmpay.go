package setting

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/pkg/gmpay"
)

// The suffix keeps the persisted credential out of the generic options API.
const GmpayConfigKey = "GmpayConfigSecret"

type GmpayAsset struct {
	Name    string `json:"name"`
	Token   string `json:"token"`
	Network string `json:"network"`
	Icon    string `json:"icon,omitempty"`
	Color   string `json:"color,omitempty"`
}

func (a GmpayAsset) Method() string { return "gmpay:" + a.Token + "." + a.Network }

type GmpayConfig struct {
	PayAddress string       `json:"pay_address"`
	MerchantID string       `json:"merchant_id"`
	SecretKey  string       `json:"secret_key"`
	Assets     []GmpayAsset `json:"assets"`
	Enabled    bool         `json:"enabled"`
}

func (c GmpayConfig) Merchant() gmpay.Config {
	return gmpay.Config{GatewayURL: c.PayAddress, MerchantID: c.MerchantID, Key: c.SecretKey}
}

func (c *GmpayConfig) Validate() error {
	c.PayAddress = strings.TrimSpace(c.PayAddress)
	c.MerchantID = strings.TrimSpace(c.MerchantID)
	if c.PayAddress != "" {
		address, err := gmpay.NormalizeGatewayURL(c.PayAddress)
		if err != nil {
			return err
		}
		c.PayAddress = address
	}
	if len(c.MerchantID) > 128 || len(c.SecretKey) > 4096 || len(c.Assets) > 100 {
		return gmpay.ErrInvalidConfig
	}
	seen := map[string]bool{}
	for i := range c.Assets {
		a := &c.Assets[i]
		a.Name = strings.TrimSpace(a.Name)
		a.Token = strings.ToLower(strings.TrimSpace(a.Token))
		a.Network = strings.ToLower(strings.TrimSpace(a.Network))
		if a.Name == "" || len(a.Name) > 128 || !validGmpayIdentifier(a.Token) || !validGmpayIdentifier(a.Network) || len(a.Icon) > 256 || len(a.Color) > 64 {
			return errors.New("invalid gmpay asset")
		}
		if seen[a.Method()] {
			return errors.New("duplicate gmpay asset")
		}
		seen[a.Method()] = true
	}
	if c.Enabled && (c.PayAddress == "" || c.MerchantID == "" || c.SecretKey == "" || len(c.Assets) == 0) {
		return gmpay.ErrInvalidConfig
	}
	return nil
}

func validGmpayIdentifier(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}
