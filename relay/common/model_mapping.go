package common

import (
	"errors"

	rootcommon "github.com/QuantumNous/new-api/common"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
)

// ModelMappingResolution retains the native helper's control-flow state as well
// as the model used by dispatch and routing observations.
type ModelMappingResolution struct {
	Name              string
	MappingConfigured bool
	Mapped            bool
	DirectSelfMapping bool
}

// ResolveMappedModelName is shared by routing observations and native counts.
// Callers that update a request use ResolveModelMapping's native exit metadata.
func ResolveMappedModelName(origin, mapping string) (string, bool, error) {
	result, err := ResolveModelMapping(origin, mapping)
	if err != nil {
		return "", false, err
	}
	return result.Name, result.Mapped, nil
}

// ResolveModelMapping is the single parser and traversal for channel mappings.
// Mapped remains true when a later edge detects a cycle, matching the native
// helper's state before it returns that error.
func ResolveModelMapping(origin, mapping string) (ModelMappingResolution, error) {
	result := ModelMappingResolution{Name: origin, MappingConfigured: mapping != "" && mapping != "{}"}
	if !result.MappingConfigured {
		return result, nil
	}
	var models map[string]string
	if err := rootcommon.Unmarshal([]byte(mapping), &models); err != nil {
		return result, errors.New("unmarshal_model_mapping_failed")
	}
	visited := map[string]bool{origin: true}
	for {
		next, exists := models[result.Name]
		if base := hostreasoning.BaseModelName(result.Name); (!exists || next == "") && base != result.Name {
			next, exists = models[base]
		}
		if !exists || next == "" {
			return result, nil
		}
		if visited[next] {
			if next == result.Name {
				result.DirectSelfMapping = result.Name == origin
				return result, nil
			}
			return result, errors.New("model_mapping_contains_cycle")
		}
		visited[next] = true
		result.Mapped = true
		result.Name = next
	}
}
