package operation_setting

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Both configured minimums are USD purchase units, independent of UI display.
func EpayMinimumUSD(global int64, method map[string]string) (int64, error) {
	if global <= 0 || global > math.MaxInt64/500000 {
		return 0, errors.New("invalid epay minimum")
	}
	if raw := strings.TrimSpace(method["min_topup"]); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 || value > math.MaxInt64/500000 {
			return 0, errors.New("invalid payment method minimum")
		}
		if value > global {
			global = value
		}
	}
	return global, nil
}

func ParseEpayMethods(raw string) ([]map[string]string, error) {
	var methods []map[string]string
	if err := common.UnmarshalJsonStr(raw, &methods); err != nil {
		return nil, errors.New("invalid epay payment methods")
	}
	seen := make(map[string]bool)
	for _, method := range methods {
		name, kind := strings.TrimSpace(method["name"]), strings.TrimSpace(method["type"])
		if name == "" || len(name) > 128 || kind == "" || len(kind) > 50 || seen[kind] {
			return nil, errors.New("invalid or duplicate epay payment method")
		}
		for _, ch := range kind {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
				return nil, errors.New("invalid epay payment method type")
			}
		}
		if _, err := EpayMinimumUSD(1, method); err != nil {
			return nil, err
		}
		method["name"], method["type"] = name, kind
		seen[kind] = true
	}
	return methods, nil
}
