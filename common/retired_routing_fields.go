package common

import (
	"fmt"
	"sort"
	"strings"
)

type RetiredRoutingFieldError struct {
	Path string
}

func (e *RetiredRoutingFieldError) Error() string {
	return "retired routing field: " + e.Path
}

// These checks apply to administrator-owned rules, never inference payloads.
func ValidateRoutingRuleJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}
	var value any
	if err := Unmarshal(data, &value); err != nil {
		return err
	}
	paths := RetiredRoutingRulePaths(value)
	if len(paths) > 0 {
		return &RetiredRoutingFieldError{Path: paths[0]}
	}
	return nil
}

func RetiredRoutingRulePaths(value any) []string {
	var paths []string
	var visit func(any, string)
	visit = func(value any, path string) {
		switch node := value.(type) {
		case []any:
			for i, item := range node {
				visit(item, fmt.Sprintf("%s[%d]", path, i))
			}
		case map[string]any:
			if _, exists := node["include_using_group"]; exists {
				paths = append(paths, path+".include_using_group")
			}
			if sources, ok := node["key_sources"].([]any); ok {
				for i, item := range sources {
					source, _ := item.(map[string]any)
					kind, _ := source["type"].(string)
					key, _ := source["key"].(string)
					if (kind == "context_string" || kind == "context_int") && retiredRoutingContextPath(key) {
						paths = append(paths, fmt.Sprintf("%s.key_sources[%d].key", path, i))
					}
				}
			}
			if operations, ok := node["operations"].([]any); ok {
				for i, item := range operations {
					op, _ := item.(map[string]any)
					opPath := fmt.Sprintf("%s.operations[%d]", path, i)
					paths = append(paths, retiredRoutingConditionPaths(op["conditions"], opPath+".conditions")...)
					if op["mode"] == "prune_objects" {
						options, _ := op["value"].(map[string]any)
						paths = append(paths, retiredRoutingConditionPaths(options["conditions"], opPath+".value.conditions")...)
						paths = append(paths, retiredRoutingConditionPaths(options["where"], opPath+".value.where")...)
					}
				}
			}
			for _, key := range []string{"rules", "param_override_template"} {
				visit(node[key], path+"."+key)
			}
		}
	}
	visit(value, "$")
	sort.Strings(paths)
	return paths
}

func retiredRoutingConditionPaths(value any, path string) []string {
	var paths []string
	switch conditions := value.(type) {
	case map[string]any:
		for key := range conditions {
			if retiredRoutingContextPath(key) {
				paths = append(paths, path+"."+key)
			}
		}
	case []any:
		for i, item := range conditions {
			condition, _ := item.(map[string]any)
			key, _ := condition["path"].(string)
			if retiredRoutingContextPath(key) {
				paths = append(paths, fmt.Sprintf("%s[%d].path", path, i))
			}
		}
	}
	return paths
}

func retiredRoutingContextPath(path string) bool {
	path = strings.TrimSpace(path)
	for _, name := range []string{"group", "using_group", "token_group", "user_group"} {
		if path == name || strings.HasPrefix(path, name+".") || strings.HasPrefix(path, name+"[") {
			return true
		}
	}
	return false
}
