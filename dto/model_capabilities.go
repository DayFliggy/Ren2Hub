package dto

import (
	"errors"
	"fmt"
	"strings"
)

// ModelCapabilities records only explicitly supplied facts. Nil slices and
// pointers mean unknown; an empty slice or false pointer is an explicit fact.
// Prices and channel routing capabilities remain separate sources of truth.
type ModelCapabilities struct {
	ReasoningEfforts []string `json:"reasoning_efforts"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	ContextLimit     *int64   `json:"context_limit"`
	OutputLimit      *int64   `json:"output_limit"`
	ToolCall         *bool    `json:"tool_call"`
	Source           *string  `json:"source"`
	UpdatedAt        int64    `json:"updated_at"`
}

func (card ModelCapabilities) Validate() error {
	for name, values := range map[string][]string{
		"reasoning_efforts": card.ReasoningEfforts,
		"input_modalities":  card.InputModalities,
		"output_modalities": card.OutputModalities,
	} {
		if len(values) > 32 {
			return fmt.Errorf("%s cannot contain more than 32 values", name)
		}
		seen := make(map[string]bool, len(values))
		for _, value := range values {
			if value == "" || len(value) > 64 || strings.TrimSpace(value) != value || seen[value] {
				return fmt.Errorf("%s must contain unique non-empty values of at most 64 bytes", name)
			}
			seen[value] = true
		}
	}
	if card.ContextLimit != nil && *card.ContextLimit <= 0 {
		return errors.New("context_limit must be positive or null")
	}
	if card.OutputLimit != nil && *card.OutputLimit <= 0 {
		return errors.New("output_limit must be positive or null")
	}
	if card.ContextLimit != nil && card.OutputLimit != nil && *card.OutputLimit > *card.ContextLimit {
		return errors.New("output_limit cannot exceed context_limit")
	}
	if card.Source != nil && (strings.TrimSpace(*card.Source) == "" || len(*card.Source) > 255) {
		return errors.New("capability source must contain between 1 and 255 bytes")
	}
	return nil
}
