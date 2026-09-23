package operation_setting

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// GmpayAsset binds a display name to the token and network requested at creation.
// The callback cannot prove the final network selected in the hosted cashier.
type GmpayAsset struct {
	Name     string `json:"name"`
	Token    string `json:"token"`
	Network  string `json:"network"`
	Icon     string `json:"icon,omitempty"`
	Color    string `json:"color,omitempty"`
	MinTopUp string `json:"min_topup,omitempty"`
}

// ParseGmpayAssets validates the configured asset catalog. Every entry must
// name a token and a network; a display-only entry cannot be collected.
func ParseGmpayAssets(raw string) ([]GmpayAsset, error) {
	var assets []GmpayAsset
	if err := common.UnmarshalJsonStr(raw, &assets); err != nil {
		return nil, errors.New("invalid gmpay asset catalog")
	}
	seen := make(map[string]bool, len(assets))
	for index := range assets {
		asset := &assets[index]
		asset.Name = strings.TrimSpace(asset.Name)
		asset.Token = strings.ToLower(strings.TrimSpace(asset.Token))
		asset.Network = strings.ToLower(strings.TrimSpace(asset.Network))
		asset.MinTopUp = strings.TrimSpace(asset.MinTopUp)
		if asset.Name == "" || len(asset.Name) > 128 {
			return nil, errors.New("invalid gmpay asset name")
		}
		if !validAssetIdentifier(asset.Token, 32) || !validAssetIdentifier(asset.Network, 32) {
			return nil, errors.New("invalid gmpay asset token or network")
		}
		// One display entry per token+network pair: duplicates would render two
		// choices that resolve to the same frozen asset.
		key := asset.Token + "." + asset.Network
		if seen[key] {
			return nil, errors.New("duplicate gmpay asset " + key)
		}
		seen[key] = true
		if _, err := GmpayMinimumUSD(1, *asset); err != nil {
			return nil, err
		}
	}
	return assets, nil
}

// GmpayMinimumUSD resolves the effective minimum in USD purchase units. Both the
// global minimum and the per-asset override are USD, matching the crypto quote
// policy; a per-asset value only ever raises the floor.
func GmpayMinimumUSD(global int64, asset GmpayAsset) (int64, error) {
	if global <= 0 || global > math.MaxInt64/500000 {
		return 0, errors.New("invalid gmpay minimum")
	}
	if raw := strings.TrimSpace(asset.MinTopUp); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 || value > math.MaxInt64/500000 {
			return 0, errors.New("invalid gmpay asset minimum")
		}
		if value > global {
			global = value
		}
	}
	return global, nil
}

// validAssetIdentifier accepts the lowercase symbol forms the gateway uses for
// tokens and networks.
func validAssetIdentifier(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}
