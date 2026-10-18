package common

import (
	"encoding/json"
	"errors"

	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
)

// ResolveMappedModelName is shared by dispatch and routing observations. The
// channel mapping, including its reasoning aliases, defines the upstream model.
func ResolveMappedModelName(origin, mapping string) (string, bool, error) {
	if mapping == "" || mapping == "{}" {
		return origin, false, nil
	}
	var models map[string]string
	if err := json.Unmarshal([]byte(mapping), &models); err != nil {
		return "", false, errors.New("unmarshal_model_mapping_failed")
	}
	current := origin
	visited := map[string]bool{current: true}
	for {
		next, exists := models[current]
		if base := hostreasoning.BaseModelName(current); (!exists || next == "") && base != current {
			next, exists = models[base]
		}
		if !exists || next == "" {
			return current, current != origin, nil
		}
		if visited[next] {
			if next == current {
				return current, current != origin, nil
			}
			return "", false, errors.New("model_mapping_contains_cycle")
		}
		visited[next] = true
		current = next
	}
}
