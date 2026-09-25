package dto

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ResolveEffectiveEffort resolves the current turn without changing the request.
// A system message's effort takes effect at the next user message and persists
// until a later system setting is activated by another user message.
func (c *ClaudeRequest) ResolveEffectiveEffort() (effort string, present bool, err error) {
	if c == nil {
		return "", false, nil
	}
	effort, present, err = parseClaudeOutputEffort(c.OutputConfig, "output_config")
	if err != nil {
		return "", false, err
	}
	var pending string
	var pendingPresent bool
	for i, message := range c.Messages {
		path := fmt.Sprintf("messages[%d]", i)
		value, hasValue, err := parseClaudeOutputEffort(message.OutputConfig, path+".output_config")
		if err != nil {
			return "", false, err
		}
		if hasValue {
			if message.Role != "system" {
				return "", false, fmt.Errorf("%s.role: message-level effort requires a system message", path)
			}
			pending, pendingPresent = value, true
		}
		if message.Role == "user" && pendingPresent {
			effort, present = pending, true
			pendingPresent = false
		}
	}
	return effort, present, nil
}

func parseClaudeOutputEffort(raw json.RawMessage, path string) (string, bool, error) {
	if len(raw) == 0 {
		return "", false, nil
	}
	var config map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &config); err != nil {
		return "", false, fmt.Errorf("%s: expected a JSON object or null", path)
	}
	rawEffort, present := config["effort"]
	if !present {
		return "", false, nil
	}
	var effort *string
	if err := kitutil.Unmarshal(rawEffort, &effort); err != nil || effort == nil || strings.TrimSpace(*effort) == "" {
		return "", false, fmt.Errorf("%s.effort: expected a non-empty string", path)
	}
	return *effort, true, nil
}
