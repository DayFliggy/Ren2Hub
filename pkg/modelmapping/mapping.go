package modelmapping

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

func Parse(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return nil, nil
	}
	mapping := make(map[string]string)
	if err := common.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil, errors.New("unmarshal_model_mapping_failed")
	}
	return mapping, nil
}

func Resolve(modelName string, mapping map[string]string) (string, bool, error) {
	if modelName == "" || len(mapping) == 0 {
		return modelName, false, nil
	}

	current := modelName
	visited := map[string]struct{}{current: {}}
	mapped := false
	for {
		next, ok := mapping[current]
		if !ok {
			next, ok = mapping[reasoning.BaseModelName(current)]
		}
		if !ok || next == "" {
			return current, mapped, nil
		}
		if next == current {
			return current, mapped, nil
		}
		if _, ok := visited[next]; ok {
			return "", false, errors.New("model_mapping_contains_cycle")
		}
		visited[next] = struct{}{}
		current = next
		mapped = true
	}
}
