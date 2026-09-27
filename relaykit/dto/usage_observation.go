package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// HasJSONTokenCounts distinguishes a confirmed zero from omitted/null usage.
// It runs on provider JSON before scalar DTO fields lose presence information.
func HasJSONTokenCounts(data []byte, names ...string) bool {
	var fields map[string]json.RawMessage
	if kitutil.Unmarshal(data, &fields) != nil {
		return false
	}
	for _, name := range names {
		value := strings.TrimSpace(string(fields[name]))
		if value == "" || value == "null" {
			return false
		}
		var count int64
		if kitutil.Unmarshal(fields[name], &count) != nil || count < 0 {
			return false
		}
	}
	return true
}

func (response *OpenAIResponsesResponse) UnmarshalJSON(data []byte) error {
	type plain OpenAIResponsesResponse
	var value plain
	if err := kitutil.Unmarshal(data, &value); err != nil {
		return err
	}
	*response = OpenAIResponsesResponse(value)
	if response.Usage == nil {
		return nil
	}
	var raw struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := kitutil.Unmarshal(data, &raw); err != nil {
		return err
	}
	complete := HasJSONTokenCounts(raw.Usage, "input_tokens", "output_tokens")
	source, terminal := "provider", "unknown"
	if !complete {
		source = "partial"
	}
	var status string
	if kitutil.Unmarshal(response.Status, &status) == nil && status == "completed" {
		terminal = "completed"
	}
	response.Usage.Observation = &UsageObservation{Source: source, Complete: complete, Terminal: terminal}
	return nil
}

func (usage *RealtimeUsage) UnmarshalJSON(data []byte) error {
	type plain RealtimeUsage
	var value plain
	if err := kitutil.Unmarshal(data, &value); err != nil {
		return err
	}
	*usage = RealtimeUsage(value)
	complete := HasJSONTokenCounts(data, "input_tokens", "output_tokens")
	usage.Observation = &UsageObservation{Source: "provider", Complete: complete, Terminal: "collecting"}
	return nil
}

// UsageObservation is assigned by the relay at the transport boundary. It is
// not decoded from provider JSON, where similarly named fields are untrusted.
type UsageObservation struct {
	Source      string
	Estimated   bool
	Complete    bool
	Terminal    string
	ResponseIDs []string
}

func (usage *RealtimeUsage) CanonicalUsage() *Usage {
	if usage == nil {
		return nil
	}
	source := "missing"
	if usage.Observation != nil {
		source = usage.Observation.Source
	}
	return &Usage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens,
		TotalTokens: usage.TotalTokens, PromptTokensDetails: usage.InputTokenDetails.Clone(),
		CompletionTokenDetails: usage.OutputTokenDetails, UsageSource: source, Observation: usage.Observation}
}
