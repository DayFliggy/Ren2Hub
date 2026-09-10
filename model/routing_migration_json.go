package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Preserve vendor extensions and exact accepted prices, including zero values
// omitted by the old TaskBillingContext serializer.
func migrateTaskBillingJSON(raw []byte, needsSettlement bool) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if needsSettlement {
			return nil, errors.New("unfinished task has no billing snapshot")
		}
		return raw, nil
	}
	var object map[string]json.RawMessage
	if err := common.Unmarshal(raw, &object); err != nil {
		return nil, errors.New("invalid task private_data JSON")
	}
	var billing map[string]json.RawMessage
	if value, ok := object["billing_context"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		if err := common.Unmarshal(value, &billing); err != nil || billing == nil {
			return nil, errors.New("invalid task billing_context JSON")
		}
	} else {
		if needsSettlement {
			return nil, errors.New("unfinished task has no billing snapshot")
		}
		return raw, nil
	}
	if err := migrateFrozenMultiplier(billing, true); err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(billing)
	if err != nil {
		return nil, err
	}
	object["billing_context"] = encoded
	return common.Marshal(object)
}

func migrateLogBillingJSON(raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return raw, nil
	}
	var object map[string]json.RawMessage
	if err := common.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("invalid log other JSON")
	}
	_, hasOld := object["group_ratio"]
	if _, exists := object["billing_multiplier"]; exists || hasOld {
		if err := migrateFrozenMultiplier(object, false); err != nil {
			return nil, err
		}
	}
	// The old user override was already included in the effective group ratio.
	// It is evidence, never an additional multiplier.
	if value, exists := object["user_group_ratio"]; exists {
		var multiplier float64
		if err := decodeFrozenMultiplier(value, &multiplier); err != nil {
			return nil, err
		}
		if other, ok := object["historical_user_multiplier"]; ok {
			if err := decodeFrozenMultiplier(other, &multiplier); err != nil {
				return nil, err
			}
			if !equalFrozenMultiplier(other, value) {
				return nil, errors.New("conflicting historical user multiplier")
			}
		}
		object["historical_user_multiplier"] = value
		delete(object, "user_group_ratio")
	}
	if value, exists := object["historical_user_multiplier"]; exists {
		var multiplier float64
		if err := decodeFrozenMultiplier(value, &multiplier); err != nil {
			return nil, err
		}
	}
	return common.Marshal(object)
}

func migrateFrozenMultiplier(object map[string]json.RawMessage, omittedZero bool) error {
	old, hasOld := object["group_ratio"]
	current, hasCurrent := object["billing_multiplier"]
	if !hasOld && !hasCurrent && !omittedZero {
		return nil
	}
	value := 0.0
	if hasOld {
		if err := decodeFrozenMultiplier(old, &value); err != nil {
			return err
		}
	}
	if hasCurrent {
		var existing float64
		if err := decodeFrozenMultiplier(current, &existing); err != nil {
			return err
		}
		if hasOld && !equalFrozenMultiplier(old, current) {
			return errors.New("conflicting frozen billing multipliers")
		}
	}
	if !hasCurrent {
		if hasOld {
			object["billing_multiplier"] = old
		} else {
			object["billing_multiplier"] = json.RawMessage("0")
		}
	}
	if _, ok := object["multiplier_source"]; !ok {
		object["multiplier_source"] = json.RawMessage(`"historical_snapshot"`)
	} else {
		var source string
		if err := common.Unmarshal(object["multiplier_source"], &source); err != nil || source == "" {
			return errors.New("invalid billing multiplier source")
		}
	}
	delete(object, "group_ratio")
	return nil
}

func equalFrozenMultiplier(left, right []byte) bool {
	a, aOK := new(big.Rat).SetString(strings.TrimSpace(string(left)))
	b, bOK := new(big.Rat).SetString(strings.TrimSpace(string(right)))
	return aOK && bOK && a.Cmp(b) == 0
}

func equivalentRoutingJSON(left, right []byte) bool {
	if bytes.Equal(left, right) {
		return true
	}
	var a, b any
	return common.Unmarshal(left, &a) == nil && common.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

// The false flag never contributed a key segment. Remove it only from known
// affinity rule objects; parameters and SDK payload fields keep their values.
func migrateAffinityRuleJSON(raw []byte) ([]byte, error) {
	switch common.GetJsonType(raw) {
	case "array":
		var rules []map[string]json.RawMessage
		if err := common.Unmarshal(raw, &rules); err != nil {
			return nil, errors.New("invalid affinity rules JSON")
		}
		for _, rule := range rules {
			if bytes.Equal(bytes.TrimSpace(rule["include_using_group"]), []byte("false")) {
				delete(rule, "include_using_group")
			}
		}
		return common.Marshal(rules)
	case "object":
		var settings map[string]json.RawMessage
		if err := common.Unmarshal(raw, &settings); err != nil {
			return nil, errors.New("invalid affinity settings JSON")
		}
		if rules, exists := settings["rules"]; exists {
			converted, err := migrateAffinityRuleJSON(rules)
			if err != nil {
				return nil, err
			}
			settings["rules"] = converted
			return common.Marshal(settings)
		}
	}
	return raw, nil
}

func decodeFrozenMultiplier(raw []byte, value *float64) error {
	if common.GetJsonType(raw) != "number" || common.Unmarshal(raw, value) != nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return fmt.Errorf("invalid frozen billing multiplier")
	}
	return nil
}
